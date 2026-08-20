package olm

import (
	"context"
	"strings"
	"testing"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureOperatorReusesExistingSubscription(t *testing.T) {
	const namespace = "existing-operators"
	subscription := readySubscription(namespace, olmv1alpha1.CSVPhaseSucceeded)
	bootstrap := fakeBootstrap(t, subscription, readyCSV(namespace, olmv1alpha1.CSVPhaseSucceeded))

	if err := bootstrap.EnsureOperator(context.Background(), "example-operator", "requested-channel", "requested-source", "requested-namespace"); err != nil {
		t.Fatal(err)
	}

	actual := &olmv1alpha1.Subscription{}
	if err := bootstrap.Get(context.Background(), client.ObjectKeyFromObject(subscription), actual); err != nil {
		t.Fatal(err)
	}
	if actual.Spec.Channel != "existing-channel" || actual.Spec.CatalogSource != "existing-source" {
		t.Fatalf("existing subscription was changed: channel=%q source=%q", actual.Spec.Channel, actual.Spec.CatalogSource)
	}
}

func TestEnsureOperatorRejectsSubscriptionNameCollision(t *testing.T) {
	const namespace = "requested-operators"
	conflict := &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: "example-operator", Namespace: namespace},
		Spec:       &olmv1alpha1.SubscriptionSpec{Package: "different-operator"},
	}
	bootstrap := fakeBootstrap(t, conflict)

	err := bootstrap.EnsureOperator(context.Background(), "example-operator", "channel", "source", namespace)
	if err == nil || !strings.Contains(err.Error(), "different package") {
		t.Fatalf("EnsureOperator() error = %v, want package collision error", err)
	}
}

func TestEnsureOperatorReturnsFailedCSV(t *testing.T) {
	const namespace = "failed-operators"
	subscription := readySubscription(namespace, olmv1alpha1.CSVPhaseFailed)
	bootstrap := fakeBootstrap(t, subscription, readyCSV(namespace, olmv1alpha1.CSVPhaseFailed))

	err := bootstrap.EnsureOperator(context.Background(), "example-operator", "existing-channel", "existing-source", namespace)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("EnsureOperator() error = %v, want failed CSV error", err)
	}
}

func fakeBootstrap(t *testing.T, objects ...client.Object) *ClusterBootstrap {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		operatorsv1.AddToScheme,
		olmv1alpha1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return &ClusterBootstrap{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
}

func readySubscription(namespace string, phase olmv1alpha1.ClusterServiceVersionPhase) *olmv1alpha1.Subscription {
	return &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-subscription", Namespace: namespace},
		Spec: &olmv1alpha1.SubscriptionSpec{
			Package:                "example-operator",
			Channel:                "existing-channel",
			CatalogSource:          "existing-source",
			CatalogSourceNamespace: OLMNamespace,
		},
		Status: olmv1alpha1.SubscriptionStatus{InstalledCSV: string(phase) + "-csv"},
	}
}

func readyCSV(namespace string, phase olmv1alpha1.ClusterServiceVersionPhase) *olmv1alpha1.ClusterServiceVersion {
	return &olmv1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{Name: string(phase) + "-csv", Namespace: namespace},
		Status: olmv1alpha1.ClusterServiceVersionStatus{
			Phase:   phase,
			Reason:  "test reason",
			Message: "test message",
		},
	}
}
