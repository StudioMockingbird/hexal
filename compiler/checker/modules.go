package checker

// Module identity, import registration, and the import-prefix rules of RFC
// 0034 Task 4. Resolution of import paths to files is the module phase's job
// (compile.go); this file assembles the alias -> canonical-id pairs every
// module scope carries while checking.

import (
	"strings"

	"hexal/compiler/parser"
)

// ModuleRegistry records the import graph of one compilation: every reachable
// module's canonical identity, its import alias -> canonical target pairs,
// and the root module. It is assembled by CheckModules before any module is
// checked and never mutated afterward.
type ModuleRegistry struct {
	modules    map[string]*moduleEntry // canonical id -> entry
	order      []string                // canonical ids, dependencies first
	entrypoint string                  // canonical id of the root module
}

// moduleEntry is one module's import table: each local alias and the
// canonical identity it names.
type moduleEntry struct {
	imports map[string]string // import alias -> canonical module id
}

// buildModuleRegistry collects every reachable module's import aliases in
// dependency order. A module whose source key is absent is skipped exactly
// like CheckModules skips it. Paths are not resolved here: a payload that
// cannot canonicalize simply is kept as written, and resolution errors belong
// to the module phase.
func buildModuleRegistry(programs map[string]parser.Program, order []string, entrypointCanonical string) *ModuleRegistry {
	registry := &ModuleRegistry{
		modules:    make(map[string]*moduleEntry, len(order)),
		order:      append([]string(nil), order...),
		entrypoint: entrypointCanonical,
	}
	for _, moduleID := range order {
		key := moduleID + ".hex"
		program, ok := programs[key]
		if !ok {
			key = moduleID
			program, ok = programs[key]
		}
		if !ok {
			continue
		}
		entry := &moduleEntry{imports: make(map[string]string)}
		// Imports are top-level items only and never statements, so a program
		// assembled from Statements alone has nothing to register.
		for _, item := range program.Items {
			importStatement, isImport := item.(parser.ImportDeclaration)
			if !isImport {
				continue
			}
			entry.imports[importStatement.Alias.Lexeme] = canonicalModuleID(strings.Trim(importStatement.Path.Lexeme, "\""))
		}
		registry.modules[moduleID] = entry
	}
	return registry
}

// canonicalModuleID maps one module-path payload (the raw string between the
// quotes) to the module's canonical identity: the final path component
// without a .hex extension. A payload that cannot canonicalize simply is kept
// as written; resolving the path to source is the module phase's job.
func canonicalModuleID(payload string) string {
	id := payload
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		id = id[slash+1:]
	}
	return strings.TrimSuffix(id, ".hex")
}
