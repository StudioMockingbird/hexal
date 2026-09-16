package generator

// Foreign (C interoperability) emission support: resolving the defining header
// of every foreign declaration in the program and choosing the includes each
// module's header must emit. A module includes a header exactly once, in first
// checked-use order, after hexal.h and the component headers and before any
// declaration that names a foreign type.

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// foreignIndex resolves the defining header of every foreign C declaration in
// the program by exact C symbol and by foreign record canonical key.
type foreignIndex struct {
	symbols map[string]checker.ForeignHeader
	records map[string]checker.ForeignHeader
}

// buildForeignIndex scans every checked program for foreign declarations. The
// program map is iterated only to fill maps, so its order never reaches
// output.
func buildForeignIndex(programs map[string]checker.Program) *foreignIndex {
	index := &foreignIndex{
		symbols: make(map[string]checker.ForeignHeader),
		records: make(map[string]checker.ForeignHeader),
	}
	for _, program := range programs {
		for _, function := range program.ForeignFunctions {
			index.symbols[function.CName] = function.Header
		}
		for _, constant := range program.ForeignConstants {
			index.symbols[constant.CName] = constant.Header
		}
		for _, global := range program.ForeignGlobals {
			index.symbols[global.CName] = global.Header
		}
		for _, record := range program.ForeignRecords {
			index.records[record.Type.CanonicalKey] = record.Header
		}
	}
	return index
}

// moduleForeignIncludes returns the foreign includes this module's header must
// emit: every header the module declares, then every header reached through a
// cross-module foreign reference or a foreign record named by an emitted
// declaration. Deduplicated by directive, in deterministic first-use order.
func moduleForeignIncludes(program checker.Program, index *foreignIndex) ([]string, error) {
	if index == nil {
		return nil, nil
	}
	seen := make(map[string]bool)
	includes := make([]string, 0)
	add := func(header checker.ForeignHeader) {
		directive := foreignIncludeDirective(header)
		if seen[directive] {
			return
		}
		seen[directive] = true
		includes = append(includes, directive)
	}
	for _, header := range program.ForeignHeaders {
		add(header)
	}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.ForeignFunctionReferenceExpression, checker.ForeignConstantExpression, checker.ForeignGlobalExpression:
				if node.Module != "" {
					if header, ok := index.symbols[node.ForeignCName]; ok {
						add(header)
					}
				}
			}
			return nil
		},
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsForeignRecord(typ) {
				if header, ok := index.records[typ.CanonicalKey]; ok {
					add(header)
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return includes, nil
}

// foreignIncludeDirective renders one header as its exact include form. The
// system and quoted forms are distinct identities and never normalized into
// each other.
func foreignIncludeDirective(header checker.ForeignHeader) string {
	if header.System {
		return "#include <" + header.Payload + ">"
	}
	return "#include \"" + header.Payload + "\""
}
