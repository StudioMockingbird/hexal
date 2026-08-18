package checker

import "hexal/compiler/parser"

// ModuleGraph is the resolved module structure of one compilation: every
// reachable module's identity, its exact logical source key, its parsed
// program, and its resolved imports. Reachability builds it once and never
// mutates it afterwards; the checker and the generator only read it.
//
// It lives in this package because both consumers already depend on the
// checker, and it carries no checker state of its own. Order and Modules have
// identical membership by construction, so a module named in the order cannot
// be missing from the map.
type ModuleGraph struct {
	Order   []string              // dependency-first canonical ids
	Modules map[string]ModuleNode // canonical id -> node; keys match Order exactly
	Root    string                // the entrypoint's canonical id
}

// ModuleNode is one module in the graph. LogicalKey is the key the caller
// supplied in the sources map: it is never derived from the canonical id,
// never reconstructed, and never a host path.
type ModuleNode struct {
	Canonical  string
	LogicalKey string
	Program    parser.Program
	Imports    []ModuleEdge // source order, already resolved and validated
	TokenCount int          // tokens observed while lexing this module
}

// ModuleEdge is one resolved import: the alias the importing module binds and
// the canonical id it names.
type ModuleEdge struct {
	Alias  string
	Target string
}

// SingleModuleGraph returns the graph of a single-module compilation, the
// shape the direct Check entry point and the generator's unit tests compile.
func SingleModuleGraph(program parser.Program) *ModuleGraph {
	return &ModuleGraph{
		Order: []string{canonicalEntrypoint},
		Modules: map[string]ModuleNode{
			canonicalEntrypoint: {
				Canonical:  canonicalEntrypoint,
				LogicalKey: entrypointLogicalKey,
				Program:    program,
			},
		},
		Root: canonicalEntrypoint,
	}
}
