package olm

import (
	"context"
	"fmt"
	"time"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	olm "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

// ClusterBootstrap holds a controller-runtime client for cluster setup operations.
type ClusterBootstrap struct {
	client.Client
}

// Bootstrap function does the basic setup on provided cluster.
// This includes Installing Operators and updating  configurations
func (cb *ClusterBootstrap) Bootstrap(ctx context.Context) error {
	if err := cb.EnsureOperators(ctx); err != nil {
		return err
	}
	return nil
}

// EnsureOperators function installs the necessary operators on the cluster
func (cb *ClusterBootstrap) EnsureOperators(ctx context.Context) error {
	logger := log.FromContext(ctx)

	logger.Info("Installing Operators")

	// Ensure Pipelines Operator
	if err := cb.EnsureOperator(ctx, config.PipelineOperatorPackageName, config.Flags.PipelineOperatorChannel, config.Flags.PipelinesOperatorNamespace); err != nil {
		return err
	}

	// Ensure Kueue Operator
	if err := cb.EnsureOperator(ctx, config.KueueOperatorPackageName, config.Flags.KueueOperatorChannel, config.Flags.KueueOperatorNamespace); err != nil {
		return err
	}

	// Ensure Cert-Manager Operator
	if err := cb.EnsureOperator(ctx, config.CertManagerOperatorPackageName, config.Flags.CertManagerOperatorChannel, config.Flags.CertManagerOperatorNamespace); err != nil {
		return err
	}

	return nil
}

// EnsureOperator checks the state of the Subscription instantly.
// If it is not fully installed, it returns an error to trigger a controller requeue.
func (cb *ClusterBootstrap) EnsureOperator(ctx context.Context, packageName, channel, subNamespace string) error {
	logger := log.FromContext(ctx)
	found := false

	//	Ensure OperatorGroup
	if err := cb.ensureOperatorGroup(ctx, subNamespace); err != nil {
		return err
	}

	// 1. Use the strongly typed SubscriptionList
	subsList := &olm.SubscriptionList{}
	subscription := &olm.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      packageName,
			Namespace: subNamespace,
		},
		Spec: &olm.SubscriptionSpec{
			Channel:                channel,
			Package:                packageName,
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
		},
	}

	// 2. List all subscriptions cluster-wide
	if err := cb.List(ctx, subsList); err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	// 3. Search for the pipeline operator natively
	for _, sub := range subsList.Items {
		// In the Go struct, the YAML 'name' field is represented as 'Package'
		if sub.Spec.Package == packageName {
			subscription.Name = sub.Name
			subscription.Namespace = sub.Namespace
			found = true
			break
		}
	}

	if !found {
		logger.Info("Subscription not found. Creating it.", "packageName", packageName)
		// Ensure Namespace
		if err := cb.EnsureNamespace(ctx, subscription.Namespace); err != nil {
			return err
		}
		err := cb.Create(ctx, subscription)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create subscription: %w", err)
		}
	} else {
		logger.Info("Subscription found", "Package", subscription.Spec.Package, "subscription", subscription.Name, "Namespace", subscription.Namespace)
	}
	logger.Info("Waiting for operator to become ready...")

	// 5. Block and poll for readiness using the strongly typed object
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 30*time.Minute, true, func(ctx context.Context) (bool, error) {
		sub := &olm.Subscription{}

		err := cb.Get(ctx, client.ObjectKey{Name: subscription.Name, Namespace: subscription.Namespace}, sub)
		if errors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		// Directly access the Status field instead of parsing unstructured maps
		if sub.Status.InstalledCSV == "" {
			logger.Info("Waiting for CSV to be installed, %v", "subscription", subscription.Name, "Status", sub.Status)
			return false, nil
		}

		logger.Info("Subscription is ready", "Namespace", subscription.Namespace, "subscription", subscription.Name, "InstalledCSV", sub.Status.InstalledCSV)
		return true, nil
	})
}

// ensureOperatorGroup Ensures that an OperatorGroup is available in the namespace. If not then it creates it.
func (cb *ClusterBootstrap) ensureOperatorGroup(ctx context.Context, namespace string) error {

	operatorGroupList := &operatorsv1.OperatorGroupList{}
	if err := cb.List(ctx, operatorGroupList, client.InNamespace(namespace)); err != nil {
		return err
	}
	if len(operatorGroupList.Items) == 0 {
		if err := cb.EnsureNamespace(ctx, namespace); err != nil {
			return err
		}
		operatorGroup := &operatorsv1.OperatorGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespace,
				Namespace: namespace,
			},
		}
		return cb.Create(ctx, operatorGroup)
	}
	return nil
}

// EnsureNamespace ensures that Namespace is available. If not then it creates it.
func (cb *ClusterBootstrap) EnsureNamespace(ctx context.Context, namespaceName string) error {
	ns := &corev1.Namespace{}

	// 1. Try to get the namespace
	err := cb.Get(ctx, client.ObjectKey{Name: namespaceName}, ns)
	if err == nil {
		klog.V(4).Infof("Namespace %s already exists.", namespaceName)
		return nil
	}

	// 2. If the error is anything OTHER than "Not Found", return the error
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get namespace %s: %w", namespaceName, err)
	}

	// 3. If we reach here, it means the namespace is Not Found. Let's create it.
	klog.Infof("Namespace %s not found. Creating it...", namespaceName)
	newNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}

	err = cb.Create(ctx, newNs)
	// We also check IsAlreadyExists just in case another process created it in the few milliseconds since our Get call
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace %s: %w", namespaceName, err)
	}

	klog.Infof("Successfully created namespace %s.", namespaceName)
	return nil
}
