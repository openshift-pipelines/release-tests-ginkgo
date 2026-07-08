// Package store provides a thread-safe scenario-scoped key-value store for integration tests.
package store

import (
	"net/http"
	"sync"

	"github.com/tektoncd/operator/test/utils"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc"
)

// scenarioStore holds data for the current test scenario.
var scenarioStore = make(map[string]any)
var suiteStore = make(map[string]any)
var mu sync.RWMutex

// Namespace returns the stored namespace for the scenario.
func Namespace() string {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := scenarioStore["namespace"].(string); ok {
		return v
	}
	return ""
}

// Clients returns the stored *Clients for the scenario, or nil if not set.
func Clients() *clients.Clients {
	mu.RLock()
	defer mu.RUnlock()
	if cs, ok := scenarioStore["clients"].(*clients.Clients); ok {
		return cs
	}
	return nil
}

// GetCRNames returns the stored ResourceNames for the scenario.
func GetCRNames() utils.ResourceNames {
	mu.RLock()
	defer mu.RUnlock()
	if names, ok := scenarioStore["crnames"].(utils.ResourceNames); ok {
		return names
	}
	return utils.ResourceNames{}
}

// SetCRNames stores the ResourceNames for the current scenario so that
// GetCRNames returns a non-empty value for test suites that do not use the
// Gherkin step framework to seed the store.
func SetCRNames(names utils.ResourceNames) {
	mu.Lock()
	defer mu.Unlock()
	scenarioStore["crnames"] = names
}

// HTTPResponse returns the stored HTTP response for the scenario.
func HTTPResponse() *http.Response {
	mu.RLock()
	defer mu.RUnlock()
	if resp, ok := scenarioStore["response"].(*http.Response); ok {
		return resp
	}
	return nil
}

// GetPayload returns the stored payload bytes for the scenario.
func GetPayload() []byte {
	mu.RLock()
	defer mu.RUnlock()
	if p, ok := scenarioStore["payload"].([]byte); ok {
		return p
	}
	return nil
}

// Opc returns the stored opc.Cmd for the suite, or panics if missing/wrong type.
func Opc() opc.Cmd {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := suiteStore["opc"].(opc.Cmd); ok {
		return v
	}
	panic("store: opc Cmd not set or wrong type")
}

// PutScenarioData stores a string value under the given key for the scenario.
func PutScenarioData(key, value string) {
	mu.Lock()
	defer mu.Unlock()
	scenarioStore[key] = value
}

// SetNamespace sets the namespace for the current scenario.
func SetNamespace(ns string) {
	mu.Lock()
	defer mu.Unlock()
	scenarioStore["namespace"] = ns
}

// PutScenarioDataSlice stores a string slice under the given key for the scenario.
func PutScenarioDataSlice(key string, value []string) {
	mu.Lock()
	defer mu.Unlock()
	scenarioStore[key] = value
}

// GetScenarioDataSlice retrieves a string slice stored under the given key.
func GetScenarioDataSlice(key string) []string {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := scenarioStore[key].([]string); ok {
		return v
	}
	return nil
}

// GetScenarioData retrieves a string stored under the given key.
func GetScenarioData(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := scenarioStore[key].(string); ok {
		return v
	}
	return ""
}

// TargetNamespace returns the stored targetNamespace for the scenario.
func TargetNamespace() string {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := scenarioStore["targetNamespace"].(string); ok {
		return v
	}
	return ""
}

// PutSuiteData stores a value under the given key for the entire test suite.
func PutSuiteData(key string, value any) {
	mu.Lock()
	defer mu.Unlock()
	suiteStore[key] = value
}

// GetSuiteData retrieves a value stored under the given key for the suite.
func GetSuiteData(key string) any {
	mu.RLock()
	defer mu.RUnlock()
	return suiteStore[key]
}
