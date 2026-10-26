package tektonkueue

import (
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPACSecretValidationRejectsMissingWrongBaselineAndPayloadWithoutLeaks(t *testing.T) {
	for _, scenario := range []string{"valid", "missing hub", "missing copy", "type", "keys", "bytes", "wrong owner", "hub UID owner", "baseline"} {
		t.Run(scenario, func(t *testing.T) {
			f, armed := preparePACFixture(t, nil)
			source, copy := generatedPACSecret("hub"), generatedPACSecret("spoke-1")
			switch scenario {
			case "type":
				copy.Type = corev1.SecretType(payloadSentinel)
			case "keys":
				delete(copy.Data, "extra")
			case "bytes":
				copy.Data[".git-credentials"] = []byte("wrong-" + payloadSentinel)
			case "wrong owner":
				copy.OwnerReferences = nil
			case "hub UID owner":
				copy.OwnerReferences[0].UID = f.run.UID
			case "baseline":
				armed.pac.baseline["spoke-1"][generatedSecret] = true
			}
			if scenario != "missing hub" {
				publishSecret(t, f, "hub", source)
			}
			if scenario != "missing copy" {
				publishSecret(t, f, "spoke-1", copy)
			}
			bindPACFixture(t, armed, f.run)
			ready, err := armed.pacReady(f.run, f.environment.Spokes[0])
			if scenario == "valid" {
				if err != nil || !ready {
					t.Fatalf("valid Secret pair was rejected: %v", err)
				}
			} else if scenario == "missing hub" || scenario == "missing copy" {
				if err != nil || ready {
					t.Fatal("missing Secret was accepted")
				}
			} else if err == nil {
				t.Fatal("invalid Secret pair was accepted")
			}
			if err != nil && strings.Contains(err.Error(), payloadSentinel) {
				t.Fatal("Secret payload leaked in validation error")
			}
		})
	}
}

func TestPACSecretIdentityAndPayloadHistoryRemainPinnedAfterVerification(t *testing.T) {
	for _, scenario := range []string{"replacement", "payload mutation", "early deletion", "wrong spoke"} {
		t.Run(scenario, func(t *testing.T) {
			f, armed := preparePACFixture(t, nil)
			publishSecret(t, f, "hub", generatedPACSecret("hub"))
			publishSecret(t, f, "spoke-1", generatedPACSecret("spoke-1"))
			bindPACFixture(t, armed, f.run)
			if ready, err := armed.pacReady(f.run, f.environment.Spokes[0]); err != nil || !ready {
				t.Fatal("could not establish verified Secret identity")
			}
			client := f.dynamic["spoke-1"].Resource(secretGVR).Namespace(wireRun.namespace)
			secret, err := client.Get(armed.ctx, generatedSecret, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "replacement":
				if err := client.Delete(armed.ctx, generatedSecret, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				secret.SetUID("replacement")
				_, err = client.Create(armed.ctx, secret, metav1.CreateOptions{})
			case "payload mutation":
				secret.Object["data"] = map[string]any{".git-credentials": "c2Vuc2l0aXZl"}
				_, err = client.Update(armed.ctx, secret, metav1.UpdateOptions{})
			case "early deletion":
				err = client.Delete(armed.ctx, generatedSecret, metav1.DeleteOptions{})
			case "wrong spoke":
				_, err = f.dynamic["spoke-2"].Resource(secretGVR).Namespace(wireRun.namespace).Create(armed.ctx, secret, metav1.CreateOptions{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := armed.watches.wait(armed.ctx, func() (bool, error) { return true, nil }); err == nil {
				t.Fatal("accepted changed Secret history")
			} else if strings.Contains(err.Error(), payloadSentinel) {
				t.Fatal("history error leaked Secret bytes")
			}
		})
	}
}

func TestPACSecretPayloadHistoryIsPinnedBeforeVerification(t *testing.T) {
	f, armed := preparePACFixture(t, nil)
	publishSecret(t, f, "hub", generatedPACSecret("hub"))
	client := f.dynamic["hub"].Resource(secretGVR).Namespace(wireRun.namespace)
	secret, err := client.Get(armed.ctx, generatedSecret, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data := secret.Object["data"].(map[string]any)
	data["git-provider-token"] = "Y2hhbmdlZA=="
	if _, err := client.Update(armed.ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := armed.watches.wait(armed.ctx, func() (bool, error) { return true, nil }); err == nil {
		t.Fatal("accepted a pre-verification Secret payload mutation")
	} else if strings.Contains(err.Error(), payloadSentinel) {
		t.Fatal("pre-verification history error leaked Secret bytes")
	}
}

func TestPACSecretBaselinePredatesTriggerAndRetainsNoPayload(t *testing.T) {
	f := executionEnvironment(t)
	armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
	armed.pac = &pacExecution{correlation: pacEvent.Correlation, queue: "test", namespace: wireRun.namespace,
		baseline: map[string]map[string]bool{}, secrets: map[string]map[string]*pacSecret{}}
	armed.watches.pac = armed.pac
	publishSecret(t, f, "spoke-1", generatedPACSecret("spoke-1"))
	if err := armed.armSecrets(); err != nil {
		t.Fatal(err)
	}
	if !armed.pac.baseline["spoke-1"][generatedSecret] || len(armed.pac.secrets["spoke-1"]) != 0 {
		t.Fatal("baseline did not preserve preexisting names separately")
	}
	for _, sub := range armed.watches.subscriptions {
		if sub.gvr == secretGVR && sub.snapshot != nil {
			t.Fatal("baseline retained preexisting credential payloads")
		}
	}
}

func TestPACComparisonChecksCompleteKeySetAndNeverFormatsValues(t *testing.T) {
	source, copy := generatedPACSecret("hub"), generatedPACSecret("spoke-1")
	copy.Data["unexpected"] = []byte(payloadSentinel)
	if err := comparePACSecrets(source, copy); err == nil {
		t.Fatal("extra Secret key was ignored")
	} else if strings.Contains(err.Error(), payloadSentinel) {
		t.Fatal("Secret value leaked")
	}
}
