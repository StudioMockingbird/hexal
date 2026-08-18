package compiler

import "testing"

// The graph is the single authority for module resolution (RFC 0076). These
// tests assert its structural invariants and that its edges carry exactly the
// path arithmetic resolveImportPath performs — the coverage the checker's
// deleted mirror used to hold, now driven through the resolver that survives.

// resolvedEdgeTarget compiles sources and returns the target the graph
// recorded for the first import of fromModule.
func resolvedEdgeTarget(t *testing.T, sources map[string]string, entrypoint, fromModule string) (string, error) {
	t.Helper()
	graph, err := reachableModules(sources, entrypoint)
	if err != nil {
		return "", err
	}
	node, ok := graph.Modules[fromModule]
	if !ok {
		t.Fatalf("graph has no node for %s; order = %v", fromModule, graph.Order)
	}
	if len(node.Imports) != 1 {
		t.Fatalf("%s has %d edges, want 1", fromModule, len(node.Imports))
	}
	return node.Imports[0].Target, nil
}

// The six path shapes the checker's mirror used to assert, each now checked
// through the graph edge the resolver produced. The bare "math/vec3" case is
// the one that differs in kind: the mirror silently accepted it, while the
// resolver — the only implementation left — rejects a non-relative path.
func TestModuleGraphEdgesCarryResolvedPaths(t *testing.T) {
	dependency := "export fun value(): Int32 do\n    return 1\nend\n"
	for _, testCase := range []struct {
		name       string
		sources    map[string]string
		entrypoint string
		fromModule string
		want       string
	}{
		{
			name:       "sibling",
			sources:    map[string]string{"app.hex": "module M = import \"./math\"\n", "math.hex": dependency},
			entrypoint: "app.hex", fromModule: "app", want: "math",
		},
		{
			name:       "sibling spelled with .hex",
			sources:    map[string]string{"app.hex": "module M = import \"./math.hex\"\n", "math.hex": dependency},
			entrypoint: "app.hex", fromModule: "app", want: "math",
		},
		{
			name:       "nested",
			sources:    map[string]string{"app.hex": "module M = import \"./graphics/shapes\"\n", "graphics/shapes.hex": dependency},
			entrypoint: "app.hex", fromModule: "app", want: "graphics/shapes",
		},
		{
			name: "parent-relative",
			sources: map[string]string{
				"graphics/app.hex": "module M = import \"../shared/tools\"\n", "shared/tools.hex": dependency,
			},
			entrypoint: "graphics/app.hex", fromModule: "graphics/app", want: "shared/tools",
		},
		{
			name: "nested under the importer's own directory",
			sources: map[string]string{
				"graphics/app.hex": "module M = import \"./shared/tools.hex\"\n", "graphics/shared/tools.hex": dependency,
			},
			entrypoint: "graphics/app.hex", fromModule: "graphics/app", want: "graphics/shared/tools",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := resolvedEdgeTarget(t, testCase.sources, testCase.entrypoint, testCase.fromModule)
			if err != nil {
				t.Fatalf("reachableModules error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("edge target = %q, want %q", got, testCase.want)
			}
		})
	}
	t.Run("bare path is not relative", func(t *testing.T) {
		sources := map[string]string{"app.hex": "module M = import \"math/vec3\"\n", "math/vec3.hex": dependency}
		if _, err := reachableModules(sources, "app.hex"); err == nil {
			t.Fatal("reachableModules accepted a non-relative import path")
		}
	})
}

// Order and Modules are one structure with one membership, every edge names a
// node that exists, and every LogicalKey is a key the caller actually
// supplied — never a reconstruction.
func TestModuleGraphInvariants(t *testing.T) {
	sources := map[string]string{
		"graphics/app.hex": "module Shapes = import \"./shapes\"\nmodule Tools = import \"../shared/tools\"\n" +
			"result: Int32 = Shapes.corners() + Tools.value()\n",
		"graphics/shapes.hex": "module Tools = import \"../shared/tools\"\n" +
			"export fun corners(): Int32 do\n    return Tools.value()\nend\n",
		"shared/tools.hex": "export fun value(): Int32 do\n    return 1\nend\n",
		"unreachable.hex":  "export fun unused(): Int32 do\n    return 0\nend\n",
	}
	graph, err := reachableModules(sources, "graphics/app.hex")
	if err != nil {
		t.Fatalf("reachableModules error = %v", err)
	}
	if graph.Root != "graphics/app" {
		t.Fatalf("Root = %q, want %q", graph.Root, "graphics/app")
	}
	if len(graph.Order) != len(graph.Modules) {
		t.Fatalf("Order has %d ids and Modules has %d nodes", len(graph.Order), len(graph.Modules))
	}
	seen := make(map[string]bool, len(graph.Order))
	for _, canonical := range graph.Order {
		node, ok := graph.Modules[canonical]
		if !ok {
			t.Fatalf("Order names %q, which Modules does not hold", canonical)
		}
		if seen[canonical] {
			t.Fatalf("Order names %q twice", canonical)
		}
		seen[canonical] = true
		if node.Canonical != canonical {
			t.Fatalf("node under %q reports canonical %q", canonical, node.Canonical)
		}
		if _, supplied := sources[node.LogicalKey]; !supplied {
			t.Fatalf("node %q carries logical key %q, which is not a supplied source key", canonical, node.LogicalKey)
		}
		for _, edge := range node.Imports {
			if _, ok := graph.Modules[edge.Target]; !ok {
				t.Fatalf("node %q imports %q as %s, which is not in the graph", canonical, edge.Target, edge.Alias)
			}
		}
	}
	if _, reached := graph.Modules["unreachable"]; reached {
		t.Fatal("the graph holds a module no import reaches")
	}
	// Dependencies precede dependents, and each node's edges are in source
	// order.
	if graph.Order[len(graph.Order)-1] != graph.Root {
		t.Fatalf("Order = %v, want the root last", graph.Order)
	}
	aliases := make([]string, 0, 2)
	for _, edge := range graph.Modules[graph.Root].Imports {
		aliases = append(aliases, edge.Alias)
	}
	if len(aliases) != 2 || aliases[0] != "Shapes" || aliases[1] != "Tools" {
		t.Fatalf("root edges = %v, want [Shapes Tools] in source order", aliases)
	}
}
