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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

const operatorInstallTimeout = 30 * time.Minute

// ClusterBootstrap holds a controller-runtime client for cluster setup operations.
type ClusterBootstrap struct {
	client.Client
}

// EnsureOperators installs or reuses the operators required by Tekton Kueue tests.
func (cb *ClusterBootstrap) EnsureOperators(ctx context.Context) error {
	operators := []struct {
		name, channel, source, namespace string
	}{
		{config.PipelineOperatorPackageName, config.Flags.PipelineOperatorChannel, config.Flags.CatalogSource, config.Flags.PipelinesOperatorNamespace},
		{config.KueueOperatorPackageName, config.Flags.KueueOperatorChannel, "redhat-operators", config.Flags.KueueOperatorNamespace},
		{config.CertManagerOperatorPackageName, config.Flags.CertManagerOperatorChannel, "redhat-operators", config.Flags.CertManagerOperatorNamespace},
	}

	for _, operator := range operators {
		log.Printf("ensuring operator package %s", operator.name)
		if err := cb.EnsureOperator(ctx, operator.name, operator.channel, operator.source, operator.namespace); err != nil {
			return fmt.Errorf("ensure operator %s: %w", operator.name, err)
		}
	}
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
