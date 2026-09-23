package specdata

import (
	"strings"
	"testing"
)

// validCoreModule is a module every field of which validateCorelib accepts. A
// malformed case copies it and changes one field.
var validCoreModule = CoreModule{
	ID: "std/demo",
	Functions: []CoreFunction{
		{
			Name:          "read",
			Params:        []CoreParam{CoreParamHeap},
			Result:        CoreResultString,
			ErrorBehavior: ErrorFallible,
			Runtime:       "hex_demo_read",
			Components:    []ComponentID{ComponentProgram},
		},
	},
}

// TestValidateCorelibAcceptsTheRegistry pins the delivered registry against the
// validator directly.
func TestValidateCorelibAcceptsTheRegistry(t *testing.T) {
	if err := validateCorelib(coreModules); err != nil {
		t.Fatalf("validateCorelib(registry) = %v, want nil", err)
	}
}

// TestValidateCorelibRejectsMalformedRecords covers the runtime-function facts
// the model adds: a known component name, a non-empty component list, an error
// behavior consistent with the result, and the pre-existing identity rules.
func TestValidateCorelibRejectsMalformedRecords(t *testing.T) {
	const (
		wantEmptyID      = "empty id"
		wantDuplicate    = "declared twice"
		wantEmptyName    = "empty name"
		wantExactlyOne   = "exactly one"
		wantUnknownError = "unknown error behavior"
		wantDisagree     = "result and error behavior disagree"
		wantNoComponent  = "names no component"
		wantUnknownComp  = "unknown component"
		wantRepeatComp   = "repeats component"
	)
	cases := []struct {
		name   string
		mutate func(*CoreModule)
		two    bool
		want   string
	}{
		{"empty module id", func(m *CoreModule) { m.ID = "" }, false, wantEmptyID},
		{"duplicate module", func(m *CoreModule) {}, true, wantDuplicate},
		{"empty function name", func(m *CoreModule) { m.Functions[0].Name = "" }, false, wantEmptyName},
		{"exactly one entry point", func(m *CoreModule) { m.Functions[0].Builtin = "demo_read" }, false, wantExactlyOne},
		{"unknown error behavior", func(m *CoreModule) { m.Functions[0].ErrorBehavior = ErrorBehavior(9) }, false, wantUnknownError},
		{"error behavior disagrees", func(m *CoreModule) {
			m.Functions[0].Result = CoreResultSize
			m.Functions[0].ErrorBehavior = ErrorFallible
		}, false, wantDisagree},
		{"runtime names no component", func(m *CoreModule) { m.Functions[0].Components = nil }, false, wantNoComponent},
		{"unknown component", func(m *CoreModule) {
			m.Functions[0].Components = []ComponentID{"nope"}
		}, false, wantUnknownComp},
		{"repeated component", func(m *CoreModule) {
			m.Functions[0].Components = []ComponentID{ComponentProgram, ComponentProgram}
		}, false, wantRepeatComp},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			module := validCoreModule
			module.Functions = append([]CoreFunction(nil), validCoreModule.Functions...)
			testCase.mutate(&module)
			registry := []CoreModule{module}
			if testCase.two {
				// The duplicate-id check needs two records; every other case
				// rejects on the crafted record alone.
				registry = append(registry, module)
			}
			err := validateCorelib(registry)
			if err == nil {
				t.Fatalf("validateCorelib accepted a malformed record")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validateCorelib error = %q, want %q", err, testCase.want)
			}
		})
	}
}

// TestCoreFunctionQueriesCloneComponents mutates a returned function's
// component slice and requires the registry to be unaffected.
func TestCoreFunctionQueriesCloneComponents(t *testing.T) {
	function, ok := CoreFunctionByModuleName("std/program", "arguments")
	if !ok {
		t.Fatal("CoreFunctionByModuleName(std/program, arguments) not found")
	}
	if len(function.Components) == 0 {
		t.Fatal("arguments records no components")
	}
	function.Components[0] = "mutated"
	again, _ := CoreFunctionByModuleName("std/program", "arguments")
	if again.Components[0] == "mutated" {
		t.Fatal("CoreFunctionByModuleName returned a component slice aliasing the registry")
	}
}
