package driver

// selection.go owns name assignment and declaration selection: generated
// Hexal names, representability, parameter naming, and the dependency
// ordering that emits types before their uses.

import (
	"fmt"
	"strings"
)

// assignNames assigns a Hexal name to every selected exact C name, coalescing
// a record with the typedef that names it.
func (importer *importer) assignNames() {
	for _, record := range importer.typedefs {
		record.hexName = importer.assignName(record.cName)
	}
	for _, record := range importer.records {
		// A `typedef struct X {...} X` pair produces one declaration: the
		// record takes the typedef's name and C spelling, and the typedef is
		// skipped.
		if hexName, ok := importer.byCName[record.cName]; ok {
			record.hexName = hexName
			record.spelling = record.cName
			importer.byCName[record.tag+" "+record.cName] = hexName
			importer.coalesced[record.cName] = true
			importer.recordNames[hexName] = true
			continue
		}
		record.hexName = importer.assignName(record.cName)
		record.spelling = record.tag + " " + record.cName
		importer.byCName[record.spelling] = record.hexName
		importer.recordNames[record.hexName] = true
	}
	for _, enum := range importer.enums {
		if enum.cName != "" {
			enum.hexName = importer.assignName(enum.cName)
			importer.byCName["enum "+enum.cName] = enum.hexName
			importer.typeCSpelling[enum.hexName] = "int32_t"
		}
		for index := range enum.enumerators {
			enum.enumerators[index].hexName = importer.assignName(enum.enumerators[index].cName)
		}
	}
	for _, function := range importer.functions {
		function.hexName = importer.assignName(function.cName)
	}
	for _, global := range importer.globals {
		global.hexName = importer.assignName(global.cName)
	}
	for _, record := range importer.typedefs {
		importer.typeNames[record.hexName] = true
	}
	for _, record := range importer.records {
		importer.typeNames[record.hexName] = true
		if !record.complete {
			importer.incompleteRecords[record.hexName] = true
		}
	}
	for _, enum := range importer.enums {
		if enum.hexName != "" {
			importer.typeNames[enum.hexName] = true
		}
	}
}

// resolveDeclarations resolves every selected declaration's types now that all
// type names are assigned. Typedefs resolve first, iterating because one
// nullable-pointer typedef can make its users inline-only as well.
func (importer *importer) resolveDeclarations() {
	for pass := 0; pass <= len(importer.typedefs); pass++ {
		changed := false
		for _, record := range importer.typedefs {
			if importer.coalesced[record.cName] {
				record.ok = false
				continue
			}
			hexal, _, ok := importer.resolveType(record.underlying)
			if !ok {
				record.ok = false
				if !importer.typeUnsupported[record.cName] {
					importer.typeUnsupported[record.cName] = true
					changed = true
				}
				continue
			}
			if hexal == record.hexName {
				// A typedef whose underlying spelling resolves back to its own
				// name has no representable target: an anonymous-struct tag the
				// binding model declares no record for. Omit it whole, the same
				// disposition as any other unsupported declaration, rather than
				// emitting a self-referential alias.
				record.ok = false
				if !importer.typeUnsupported[record.cName] {
					importer.typeUnsupported[record.cName] = true
					changed = true
				}
				continue
			}
			record.hexal = hexal
			record.ok = true
			if strings.Contains(hexal, "|") || importer.incompleteRecords[hexal] {
				// The binding model has no transparent alias for a nullable pointer or
				// an opaque record value: resolve the name inline and emit no
				// declaration.
				if !record.inline || importer.byCName[record.cName] != hexal {
					changed = true
				}
				record.inline = true
				importer.byCName[record.cName] = hexal
				continue
			}
			if importer.byCName[record.cName] != record.hexName {
				changed = true
				importer.byCName[record.cName] = record.hexName
			}
			importer.typeCSpelling[record.hexName] = importer.ownSpelling(hexal)
		}
		if !changed {
			break
		}
	}
	for _, record := range importer.records {
		record.ok = true
		importer.typeCSpelling[record.hexName] = record.spelling
		for index := range record.fields {
			field := &record.fields[index]
			field.hexName = importer.assignName(field.cName)
			field.mutable = !hasConstQualifier(field.written)
			hexal, spelling, ok := importer.resolveType(field.written)
			if !ok {
				record.ok = false
				break
			}
			field.hexal = hexal
			field.spelling = spelling
		}
		if !record.ok {
			importer.typeUnsupported[record.spelling] = true
			importer.typeUnsupported[record.hexName] = true
		}
	}
	for _, enum := range importer.enums {
		enum.ok = true
	}
	for _, function := range importer.functions {
		function.ok = true
		for index := range function.parameters {
			parameter := &function.parameters[index]
			parameter.hexName = importer.parameterName(parameter.cName, index)
			hexal, spelling, ok := importer.resolveType(parameter.written)
			if !ok {
				function.ok = false
				break
			}
			parameter.hexal = hexal
			parameter.spelling = spelling
		}
		if !function.ok || function.noResult {
			continue
		}
		hexal, spelling, ok := importer.resolveType(function.resultWritten)
		if !ok {
			function.ok = false
			continue
		}
		function.result = hexal
		function.resultSpelling = spelling
	}
	for _, global := range importer.globals {
		hexal, spelling, ok := importer.resolveType(global.written)
		global.hexal = hexal
		global.spelling = spelling
		global.ok = ok
	}
}

// parameterName derives one parameter's Hexal name. An unnamed parameter gets
// the deterministic positional spelling; a name that cannot be a Hexal
// identifier is escaped.
func (importer *importer) parameterName(cName string, index int) string {
	if cName == "" {
		return fmt.Sprintf("arg%d", index)
	}
	if usableHexalName(cName) && !importer.used[cName] {
		return cName
	}
	return importer.escape(cName)
}

// typeDecl is one emitted type declaration: a record, a typedef, or a named
// enum type. It exists only so records, typedefs, and enum types can be
// emitted in one dependency order.
type typeDecl struct {
	record  *recordRecord
	typedef *typedefRecord
	enum    *enumRecord
	name    string
	deps    []string
}

// orderedTypeDecls returns records, typedefs, and named enum types in one
// dependency order: every type a declaration names appears first. A record
// field may name a typedef or enum, and a typedef may name a record, so the
// three families cannot be emitted in separate passes without breaking the
// declaration-before-use rule.
func (importer *importer) orderedTypeDecls() []typeDecl {
	seen := make(map[string]bool)
	items := make([]typeDecl, 0, len(importer.records)+len(importer.typedefs)+len(importer.enums))
	for _, record := range importer.records {
		if !record.ok || seen[record.hexName] {
			continue
		}
		seen[record.hexName] = true
		dependencies := make([]string, 0)
		for _, field := range record.fields {
			dependencies = append(dependencies, importer.referencedNames(field.hexal)...)
		}
		items = append(items, typeDecl{record: record, name: record.hexName, deps: dependencies})
	}
	for _, record := range importer.typedefs {
		if !record.ok || record.inline || importer.coalesced[record.cName] || seen[record.hexName] {
			continue
		}
		seen[record.hexName] = true
		items = append(items, typeDecl{typedef: record, name: record.hexName, deps: importer.referencedNames(record.hexal)})
	}
	for _, enum := range importer.enums {
		if !enum.ok || enum.cName == "" || seen[enum.hexName] {
			continue
		}
		seen[enum.hexName] = true
		items = append(items, typeDecl{enum: enum, name: enum.hexName})
	}
	return topoSort(items,
		func(item typeDecl) string { return item.name },
		func(item typeDecl) []string { return item.deps })
}

// referencedNames reports every Hexal type name mentioned in one resolved type
// expression.
func (importer *importer) referencedNames(expression string) []string {
	names := make([]string, 0)
	for _, token := range strings.FieldsFunc(expression, func(character rune) bool {
		identifier := character == '_' ||
			character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		return !identifier
	}) {
		if importer.typeNames[token] {
			names = append(names, token)
		}
	}
	return names
}

// topoSort orders items so that every dependency appears before its dependent.
// A dependency cycle degrades to source order rather than looping.
func topoSort[T any](items []T, name func(T) string, dependencies func(T) []string) []T {
	byName := make(map[string]T, len(items))
	for _, item := range items {
		byName[name(item)] = item
	}
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(items))
	result := make([]T, 0, len(items))
	var visit func(string)
	visit = func(key string) {
		if state[key] != unvisited {
			return
		}
		item, ok := byName[key]
		if !ok {
			return
		}
		state[key] = visiting
		for _, dependency := range dependencies(item) {
			visit(dependency)
		}
		state[key] = done
		result = append(result, item)
	}
	for _, item := range items {
		visit(name(item))
	}
	return result
}
