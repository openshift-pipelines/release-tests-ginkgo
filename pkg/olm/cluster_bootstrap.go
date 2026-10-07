package olm

import (
	"context"
	"fmt"
	"log"
	"time"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

const operatorInstallTimeout = 30 * time.Minute

// ClusterBootstrap holds a controller-runtime client for cluster setup operations.
type ClusterBootstrap struct {
	client.Client
}

// OperatorPrerequisite pairs an OLM package with the API group version it
// registers, so callers can install an operator and wait for it to be usable.
type OperatorPrerequisite struct {
	Package       string
	Channel       string
	CatalogSource string
	Namespace     string
	GroupVersion  string
}

// PipelinePrerequisite describes the OpenShift Pipelines operator.
func PipelinePrerequisite() OperatorPrerequisite {
	return OperatorPrerequisite{
		Package:       config.PipelineOperatorPackageName,
		Channel:       config.Flags.PipelineOperatorChannel,
		CatalogSource: config.Flags.CatalogSource,
		Namespace:     config.Flags.PipelinesOperatorNamespace,
		GroupVersion:  "operator.tekton.dev/v1alpha1",
	}
}

// MulticlusterPrerequisites describes the operators that multi-cluster work needs
// on top of Pipelines. The Kueue entry names kueue.openshift.io rather than
// kueue.x-k8s.io because the latter's CRDs are installed by the operand and only
// exist once a Kueue CR has been created.
func MulticlusterPrerequisites() []OperatorPrerequisite {
	return []OperatorPrerequisite{
		{
			Package:       config.CertManagerOperatorPackageName,
			Channel:       config.Flags.CertManagerOperatorChannel,
			CatalogSource: "redhat-operators",
			Namespace:     config.Flags.CertManagerOperatorNamespace,
			GroupVersion:  "cert-manager.io/v1",
		},
		{
			Package:       config.KueueOperatorPackageName,
			Channel:       config.Flags.KueueOperatorChannel,
			CatalogSource: "redhat-operators",
			Namespace:     config.Flags.KueueOperatorNamespace,
			GroupVersion:  "kueue.openshift.io/v1",
		},
	}
}

// EnsurePrerequisites installs or reuses each operator and, when discoveryClient is
// non-nil, waits for its API to be served before moving on. Pass nil to install
// without waiting.
func (cb *ClusterBootstrap) EnsurePrerequisites(ctx context.Context, discoveryClient discovery.DiscoveryInterface, prerequisites []OperatorPrerequisite) error {
	for _, prerequisite := range prerequisites {
		log.Printf("ensuring operator package %s", prerequisite.Package)
		if err := cb.EnsureOperator(ctx, prerequisite.Package, prerequisite.Channel,
			prerequisite.CatalogSource, prerequisite.Namespace); err != nil {
			return fmt.Errorf("ensure operator %s: %w", prerequisite.Package, err)
		}
		if discoveryClient == nil {
			continue
		}
		if err := WaitForAPI(ctx, discoveryClient, prerequisite.GroupVersion); err != nil {
			return fmt.Errorf("ensure operator %s: %w", prerequisite.Package, err)
		}
	}
	return nil
}

// EnsureOperators installs or reuses the operators required by Tekton Kueue tests.
// It does not wait for their APIs; call EnsurePrerequisites with a discovery client
// when the APIs are used immediately afterwards.
func (cb *ClusterBootstrap) EnsureOperators(ctx context.Context) error {
	return cb.EnsurePrerequisites(ctx, nil,
		append([]OperatorPrerequisite{PipelinePrerequisite()}, MulticlusterPrerequisites()...))
}

// WaitForAPI blocks until groupVersion is served by the API server.
//
// An operator's CSV reaches Succeeded before its CRDs finish registering, so a
// caller that uses the operator's API immediately after EnsureOperator can see
// "could not find the requested resource". That error is not IsNotFound, so it
// propagates instead of being treated as a missing object.
func WaitForAPI(ctx context.Context, discoveryClient discovery.DiscoveryInterface, groupVersion string) error {
	if err := wait.PollUntilContextTimeout(ctx, config.APIRetry, operatorInstallTimeout, true,
		func(context.Context) (bool, error) {
			_, err := discoveryClient.ServerResourcesForGroupVersion(groupVersion)
			return err == nil, nil
		}); err != nil {
		return fmt.Errorf("API %s was never served: %w", groupVersion, err)
	}
	log.Printf("API %s is served", groupVersion)
	return nil
}

// EnsureOperator creates an OLM Subscription when needed and waits for its CSV to succeed.
// Existing subscriptions are reused without changing their channel or catalog source.
func (cb *ClusterBootstrap) EnsureOperator(ctx context.Context, packageName, channel, catalogSource, namespace string) error {
	if cb == nil || cb.Client == nil {
		return fmt.Errorf("cannot ensure operator %s with a nil client", packageName)
	}

	subscriptions := &olmv1alpha1.SubscriptionList{}
	if err := cb.List(ctx, subscriptions); err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}

	var matches []olmv1alpha1.Subscription
	for i := range subscriptions.Items {
		subscription := subscriptions.Items[i]
		if subscription.Spec != nil && subscription.Spec.Package == packageName {
			matches = append(matches, subscription)
		}
	}
	if len(matches) > 1 {
		return fmt.Errorf("found %d subscriptions for package %s", len(matches), packageName)
	}

	var subscription *olmv1alpha1.Subscription
	if len(matches) == 1 {
		subscription = matches[0].DeepCopy()
		if subscription.Spec.Channel != channel || subscription.Spec.CatalogSource != catalogSource || subscription.Spec.CatalogSourceNamespace != OLMNamespace {
			log.Printf("reusing existing subscription %s/%s for %s (channel=%s, source=%s/%s)", subscription.Namespace, subscription.Name, packageName, subscription.Spec.Channel, subscription.Spec.CatalogSourceNamespace, subscription.Spec.CatalogSource)
		}
	} else {
		if err := cb.ensureOperatorGroup(ctx, namespace); err != nil {
			return err
		}
		subscription = &olmv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{Name: packageName, Namespace: namespace},
			Spec: &olmv1alpha1.SubscriptionSpec{
				Channel:                channel,
				Package:                packageName,
				CatalogSource:          catalogSource,
				CatalogSourceNamespace: OLMNamespace,
				InstallPlanApproval:    olmv1alpha1.ApprovalAutomatic,
			},
		}
		if err := cb.Create(ctx, subscription); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("create subscription %s/%s: %w", namespace, packageName, err)
			}
			existing := &olmv1alpha1.Subscription{}
			if err := cb.Get(ctx, client.ObjectKeyFromObject(subscription), existing); err != nil {
				return fmt.Errorf("get existing subscription %s/%s: %w", namespace, packageName, err)
			}
			if existing.Spec == nil || existing.Spec.Package != packageName {
				return fmt.Errorf("subscription %s/%s already exists for a different package", namespace, packageName)
			}
			subscription = existing
		}
	}

	key := client.ObjectKeyFromObject(subscription)
	if err := cb.waitForOperator(ctx, key, packageName); err != nil {
		return fmt.Errorf("wait for subscription %s/%s: %w", key.Namespace, key.Name, err)
	}
	return nil
}

func (cb *ClusterBootstrap) waitForOperator(ctx context.Context, key client.ObjectKey, packageName string) error {
	return wait.PollUntilContextTimeout(ctx, config.APIRetry, operatorInstallTimeout, true, func(ctx context.Context) (bool, error) {
		subscription := &olmv1alpha1.Subscription{}
		if err := cb.Get(ctx, key, subscription); apierrors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		if subscription.Status.InstalledCSV == "" {
			return false, nil
		}

		csv := &olmv1alpha1.ClusterServiceVersion{}
		csvKey := client.ObjectKey{Name: subscription.Status.InstalledCSV, Namespace: subscription.Namespace}
		if err := cb.Get(ctx, csvKey, csv); apierrors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}

		switch csv.Status.Phase {
		case olmv1alpha1.CSVPhaseSucceeded:
			log.Printf("operator package %s is ready as %s/%s", packageName, csv.Namespace, csv.Name)
			return true, nil
		case olmv1alpha1.CSVPhaseFailed:
			return false, fmt.Errorf("CSV %s/%s failed: %s: %s", csv.Namespace, csv.Name, csv.Status.Reason, csv.Status.Message)
		default:
			return false, nil
		}
	})
}

func (cb *ClusterBootstrap) ensureOperatorGroup(ctx context.Context, namespace string) error {
	if err := cb.EnsureNamespace(ctx, namespace); err != nil {
		return err
	}

	operatorGroups := &operatorsv1.OperatorGroupList{}
	if err := cb.List(ctx, operatorGroups, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list OperatorGroups in %s: %w", namespace, err)
	}
	if len(operatorGroups.Items) > 1 {
		return fmt.Errorf("found %d OperatorGroups in namespace %s", len(operatorGroups.Items), namespace)
	}
	if len(operatorGroups.Items) == 1 {
		return nil
	}

	operatorGroup := &operatorsv1.OperatorGroup{ObjectMeta: metav1.ObjectMeta{Name: namespace, Namespace: namespace}}
	if err := cb.Create(ctx, operatorGroup); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create OperatorGroup in %s: %w", namespace, err)
	}
	return nil
}

// EnsureNamespace creates a namespace when it does not already exist.
func (cb *ClusterBootstrap) EnsureNamespace(ctx context.Context, namespace string) error {
	existing := &corev1.Namespace{}
	if err := cb.Get(ctx, client.ObjectKey{Name: namespace}, existing); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", namespace, err)
	}

	created := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	if err := cb.Create(ctx, created); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", namespace, err)
	}
	return nil
}
