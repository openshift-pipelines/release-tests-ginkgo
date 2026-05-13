package olm

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"os"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	opv1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// Interval specifies the time between two polls.
	Interval = 10 * time.Second
	// Timeout specifies the timeout for the function PollImmediate to reach a certain status.
	Timeout            = 8 * time.Minute
	OperatorsNamespace = "openshift-operators"
)

type SubscriptionRequest struct {
	Name              string
	OperatorNamespace string
	*v1alpha1.SubscriptionSpec
}

func SubscribeAndWaitForOperatorToBeReady(cs *clients.Clients, sr SubscriptionRequest, kubeContext string) (*v1alpha1.Subscription, error) {
	ctx := context.Background()
	setSubscriptionDefaults(&sr)

	// Check If  namespace  already exists.
	err := ensureOperatorNamespace(cs, sr, ctx)
	if err != nil {
		return nil, err
	}

	if err := ensureOperatorGroup(cs, ctx, sr); err != nil {
		return nil, fmt.Errorf("failed to create sr: %w", err)
	}

	// Ensure Subscription
	if _, err := CreateSubscription(cs, ctx, sr); err != nil {
		return nil, fmt.Errorf("failed to create Subscription: %w", err)
	}

	subs, err := WaitForSubscriptionState(cs, sr, IsSubscriptionInstalledCSVPresent)
	if err != nil {
		return nil, err
	}
	//
	csvName := subs.Status.InstalledCSV
	_, err = WaitForClusterServiceVersionState(cs, csvName, sr.OperatorNamespace, IsCSVSucceeded)
	if err != nil {
		return nil, err
	}
	return subs, nil
}

func ensureOperatorNamespace(cs *clients.Clients, subscription SubscriptionRequest, ctx context.Context) error {
	nsClient := cs.KubeClient.Kube.CoreV1().Namespaces()
	_, err := nsClient.Get(ctx, subscription.OperatorNamespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			ns := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: subscription.OperatorNamespace,
				},
			}
			_, err = nsClient.Create(ctx, ns, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	fmt.Printf("Operator namespace %s already exists\n", subscription.OperatorNamespace)
	return nil
}

func ensureOperatorGroup(cs *clients.Clients, ctx context.Context, sr SubscriptionRequest) error {
	operatorClient := cs.OLM.OperatorsV1().OperatorGroups(sr.OperatorNamespace)
	ogl, err := operatorClient.List(ctx, metav1.ListOptions{})

	if err != nil {
		return err
	}
	if len(ogl.Items) == 0 {
		log.Printf("No operatorgroups found in namespace %s", sr.OperatorNamespace)
		og := &opv1.OperatorGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: sr.Name,
			},
		}
		_, err := operatorClient.Create(ctx, og, metav1.CreateOptions{})
		return err
	}
	fmt.Printf("Operator group %s already exists\n", sr.Name)
	return nil
}

func UptadeSubscriptionAndWaitForOperatorToBeReady(cs *clients.Clients, subscription SubscriptionRequest) (*v1alpha1.Subscription, error) {
	setSubscriptionDefaults(&subscription)
	if _, err := UpgradeSubscription(cs, subscription); err != nil {
		return nil, err
	}

	subs, err := WaitForSubscriptionState(cs, subscription, IsSubscriptionInstalledCSVPresent)
	if err != nil {
		return nil, err
	}

	csvName := subs.Status.InstalledCSV

	_, err = WaitForClusterServiceVersionState(cs, csvName, subscription.OperatorNamespace, IsCSVSucceeded)
	if err != nil {
		return nil, err
	}
	return subs, nil
}

func getSubcription(cs *clients.Clients, s SubscriptionRequest) (*v1alpha1.Subscription, error) {
	subscription, err := cs.OLM.OperatorsV1alpha1().Subscriptions(s.OperatorNamespace).Get(context.Background(), s.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription %s in namespace %s: %w", s.Name, s.OperatorNamespace, err)
	}
	return subscription, nil
}

// OperatorCleanup deletes All related CSVs, subscription & installplan from cluster
func OperatorCleanup(cs *clients.Clients, s SubscriptionRequest) error {
	sub, err := getSubcription(cs, s)
	if err != nil {
		return fmt.Errorf("failed to get subscription for cleanup: %w", err)
	}

	// Delete CSV
	err = cs.OLM.OperatorsV1alpha1().ClusterServiceVersions(s.OperatorNamespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete CSVs in namespace %s: %w", s.OperatorNamespace, err)
	}

	log.Printf("Output %s \n", cmd.MustSucceed(
		"oc", "delete", "--ignore-not-found", "-n", s.OperatorNamespace,
		"subscription", sub.Name,
	).Stdout())
	return nil
}

func UpgradeSubscription(cs *clients.Clients, sr SubscriptionRequest) (*v1alpha1.Subscription, error) {
	log.Printf("Updating subscription %s in namespace %s", sr.Name, sr.OperatorNamespace)
	subscription, err := getSubcription(cs, sr)
	if err != nil {
		return nil, err
	}
	subscription.Spec.Channel = sr.SubscriptionSpec.Channel
	subs, err := cs.OLM.OperatorsV1alpha1().Subscriptions(sr.OperatorNamespace).Update(context.Background(), subscription, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return subs, nil
}

func WaitForSubscriptionState(cs *clients.Clients, sr SubscriptionRequest, inState func(s *v1alpha1.Subscription, err error) (bool, error)) (*v1alpha1.Subscription, error) {
	var lastState *v1alpha1.Subscription
	var err error
	waitErr := wait.PollUntilContextTimeout(cs.Ctx, Interval, Timeout, true, func(context.Context) (bool, error) {
		lastState, err = cs.OLM.OperatorsV1alpha1().Subscriptions(sr.OperatorNamespace).Get(context.Background(), sr.Name, metav1.GetOptions{})
		return inState(lastState, err)
	})

	if waitErr != nil {
		return lastState, errors.Wrapf(waitErr, "subscription %s is not in desired state, got: %+v", sr.Name, lastState)
	}
	return lastState, nil
}

func WaitForClusterServiceVersionState(cs *clients.Clients, name, namespace string, inState func(s *v1alpha1.ClusterServiceVersion, err error) (bool, error)) (*v1alpha1.ClusterServiceVersion, error) {
	var lastState *v1alpha1.ClusterServiceVersion
	var err error
	waitErr := wait.PollUntilContextTimeout(cs.Ctx, Interval, Timeout, true, func(context.Context) (bool, error) {
		lastState, err = cs.OLM.OperatorsV1alpha1().ClusterServiceVersions(namespace).Get(context.Background(), name, metav1.GetOptions{})
		return inState(lastState, err)
	})

	if waitErr != nil {
		return lastState, errors.Wrapf(waitErr, "clusterserviceversion %s is not in desired state, got: %+v", name, lastState)
	}
	return lastState, nil
}

func IsCSVSucceeded(c *v1alpha1.ClusterServiceVersion, err error) (bool, error) {
	return c.Status.Phase == "Succeeded", err
}

func IsSubscriptionInstalledCSVPresent(s *v1alpha1.Subscription, err error) (bool, error) {
	log.Printf("Subscription %s is installed CSV: %s\n", s.Name, s.Status.InstalledCSV)
	return s.Status.InstalledCSV != "" && s.Status.InstalledCSV != "<none>", err
}

func CreateSubscription(cs *clients.Clients, ctx context.Context, sr SubscriptionRequest) (*v1alpha1.Subscription, error) {

	setSubscriptionDefaults(&sr)

	subscriptionInterface := cs.OLM.OperatorsV1alpha1().Subscriptions(sr.OperatorNamespace)
	subscription, err := subscriptionInterface.Get(ctx, sr.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			subscription = &v1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sr.Name,
					Namespace: sr.OperatorNamespace,
				},
				Spec: sr.SubscriptionSpec,
			}
			_, err = subscriptionInterface.Create(ctx, subscription, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to create subscription %s in namespace %s: %w", sr.Name, sr.OperatorNamespace, err)
			}

		} else {
			return nil, err
		}
	} else {
		return UpgradeSubscription(cs, sr)
	}
	return subscription, nil
}

func setSubscriptionDefaults(sr *SubscriptionRequest) {
	if sr.Channel == "" {
		sr.Channel = "latest"
	}
	if sr.OperatorNamespace == "" {
		sr.OperatorNamespace = OperatorsNamespace
	}
	if sr.CatalogSourceNamespace == "" {
		sr.CatalogSourceNamespace = "openshift-marketplace"
	}

}

func fromTemplate(templateFile, outputFile string, sr any) (string, error) {
	tmpl, err := config.Read(templateFile)
	if err != nil {
		return "", fmt.Errorf("failed to read template:%s, %w", templateFile, err)
	}

	sub, err := template.New("subscription").Parse(string(tmpl))
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buffer bytes.Buffer
	if err = sub.Execute(&buffer, sr); err != nil {
		return "", fmt.Errorf("failed to execute  template: %w", err)
	}
	file, err := config.TempFile(outputFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	if err = os.WriteFile(file, buffer.Bytes(), 0600); err != nil {
		return "", fmt.Errorf("failed to write subscription file: %w", err)
	}
	return file, nil
}
