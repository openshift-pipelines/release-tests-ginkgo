package tektonkueue

import (
	"reflect"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func TestRoutingExpressionSelectsControllerAndQueue(t *testing.T) {
	// Given: bind the product helper signatures to observable routing decisions.
	// This tests our CEL branching, not the installed webhook implementation.
	env, err := cel.NewEnv(
		cel.Variable("pipelineRun", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("plrNamespace", cel.StringType),
		cel.Function("multiKueue",
			cel.Overload("remote_default", nil, cel.StringType,
				cel.FunctionBinding(func(...ref.Val) ref.Val { return types.String("remote:test") })),
			cel.Overload("remote_queue", []*cel.Type{cel.StringType}, cel.StringType,
				cel.UnaryBinding(func(queue ref.Val) ref.Val { return types.String("remote:" + queue.Value().(string)) }))),
		cel.Function("label",
			cel.Overload("local_queue", []*cel.Type{cel.StringType, cel.StringType}, cel.StringType,
				cel.BinaryBinding(func(_, queue ref.Val) ref.Val { return types.String("local:" + queue.Value().(string)) }))),
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg := (&Environment{Prefix: "test", Namespace: "test"}).routingConfig(true)
	if cfg.MultiKueueOverride {
		t.Fatal("CEL routing is masked by the legacy override")
	}
	ast, issues := env.Compile(cfg.CEL.Expressions[0])
	if issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		namespace string
		labels    map[string]string
		want      []string
	}{
		{"remote", "test", map[string]string{routeLabel: "remote"}, []string{"remote:test"}},
		{"explicit", "test", map[string]string{routeLabel: "explicit", queueLabel: "test-local"}, []string{"remote:test-explicit"}},
		{"unmatched", "test", map[string]string{routeLabel: "local"}, []string{"local:test-local"}},
		{"missing route label", "test", map[string]string{"other": "value"}, []string{"local:test-local"}},
		{"no labels", "test", nil, []string{"local:test-local"}},
		{"other namespace", "other", map[string]string{routeLabel: "remote"}, []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata := map[string]any{"name": "test-run"}
			if test.labels != nil {
				metadata["labels"] = test.labels
			}
			// When
			result, _, err := program.Eval(map[string]any{
				"plrNamespace": test.namespace, "pipelineRun": map[string]any{"metadata": metadata},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := result.ConvertToNative(reflect.TypeFor[[]string]())
			// Then
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("routing decision = %v, %v; want %v", got, err, test.want)
			}
		})
	}
}
