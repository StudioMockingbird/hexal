package driver

// emit.go owns macro constants and extern-c module emission: the scalar
// and string-array predicates, the emitter, and coalescing, ordering, and
// dedupe over the collected records.

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// macroRecord is one object-like macro emitted as a foreign constant.
type macroRecord struct {
	cName   string
	hexName string
	hexal   string
}

// macroConstants resolves every object-like macro Clang typed to a supported
// foreign-constant type. A macro whose type resolves to a pointer, a complete
// foreign record, or a scalar is emitted; a function pointer, an opaque
// record, or an unrepresentable type is omitted whole. Names are assigned
// here, after declarations, and the result is sorted for deterministic output.
func (importer *importer) macroConstants() []macroRecord {
	names := slices.Sorted(maps.Keys(importer.macroTypes))
	constants := make([]macroRecord, 0, len(names))
	for _, name := range names {
		qualType := importer.macroTypes[name]
		hexal, _, ok := importer.resolveType(qualType)
		if !ok {
			// A C string literal has array type char[N]; it decays to a
			// read-only byte pointer.
			if stringArrayType(qualType) {
				hexal, ok = "Ptr<Byte> | Nil", true
			}
		}
		if !ok || !importer.foreignConstantType(hexal) {
			continue
		}
		constants = append(constants, macroRecord{cName: name, hexName: importer.assignName(name), hexal: hexal})
	}
	return constants
}

// scalarHexalType reports whether one Hexal type name is a builtin scalar a
// foreign constant may name.
func scalarHexalType(name string) bool {
	switch name {
	case "Bool", "Int8", "Int16", "Int32", "Int64",
		"UInt8", "UInt16", "UInt32", "UInt64",
		"Byte", "Float32", "Float64", "Size":
		return true
	default:
		return false
	}
}

// scalarCSpelling is the set of C spellings a scalar foreign constant may
// carry.
var scalarCSpelling = map[string]bool{
	"bool": true, "int8_t": true, "int16_t": true, "int32_t": true, "int64_t": true,
	"uint8_t": true, "uint16_t": true, "uint32_t": true, "uint64_t": true,
	"float": true, "double": true, "size_t": true,
}

// foreignConstantType reports whether one resolved Hexal type may be a foreign
// constant's type, matching the checker's accepted set: a builtin scalar, a
// scalar transparent alias (an enum or scalar typedef), a data pointer, or a
// complete foreign record. An opaque record and a function pointer are
// rejected.
func (importer *importer) foreignConstantType(hexal string) bool {
	if scalarHexalType(hexal) || scalarCSpelling[importer.typeCSpelling[hexal]] {
		return true
	}
	if strings.HasPrefix(hexal, "Ptr<") && strings.HasSuffix(hexal, "> | Nil") {
		return true
	}
	return importer.recordNames[hexal] && !importer.incompleteRecords[hexal]
}

// stringArrayType reports whether one C type spelling is a char array, the
// type of a C string literal.
func stringArrayType(qualType string) bool {
	trimmed := strings.TrimSpace(qualType)
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "const "))
	if !strings.HasPrefix(trimmed, "char") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "char"))
	return strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]")
}

// emit renders the binding module.
func (importer *importer) emit() string {
	importer.coalesceRedeclarations()
	importer.assignNames()
	importer.resolveDeclarations()

	var body strings.Builder
	exported := make([]string, 0)
	include := "\"" + importer.request.Header + "\""
	if importer.request.System {
		include = "<" + importer.request.Header + ">"
	}
	fmt.Fprintf(&body, "extern c from %s do\n", include)

	// Records, typedefs, and named enum types are emitted in one dependency
	// order, so a record field that names a typedef or enum is emitted after
	// that type and a typedef that names a record is emitted after it. The
	// constants that use those types follow.
	for _, decl := range importer.orderedTypeDecls() {
		switch {
		case decl.record != nil:
			record := decl.record
			if !record.complete {
				fmt.Fprintf(&body, "    type %s as %q is opaque\n", record.hexName, record.spelling)
				exported = append(exported, record.hexName)
				continue
			}
			fmt.Fprintf(&body, "    type %s as %q is struct\n", record.hexName, record.spelling)
			for _, field := range record.fields {
				qualifier := ""
				if field.mutable {
					qualifier = "mut "
				}
				// A record field records no independent C type spelling: the
				// extern-member `as` names the C field, and the field's C
				// representation follows from its Hexal type.
				fmt.Fprintf(&body, "        %s%s: %s,\n", qualifier, field.hexName, field.hexal)
			}
			body.WriteString("    end\n")
			exported = append(exported, record.hexName)
		case decl.typedef != nil:
			fmt.Fprintf(&body, "    type %s is %s\n", decl.typedef.hexName, decl.typedef.hexal)
			exported = append(exported, decl.typedef.hexName)
		case decl.enum != nil:
			fmt.Fprintf(&body, "    type %s is Int32\n", decl.enum.hexName)
			exported = append(exported, decl.enum.hexName)
		}
	}
	for _, enum := range importer.enums {
		if !enum.ok {
			continue
		}
		for _, enumerator := range enum.enumerators {
			typeName := "Int32"
			if enum.cName != "" {
				typeName = enum.hexName
			}
			fmt.Fprintf(&body, "    constant %s as %q: %s\n", enumerator.hexName, enumerator.cName, typeName)
			exported = append(exported, enumerator.hexName)
		}
	}
	for _, macro := range importer.macroConstants() {
		fmt.Fprintf(&body, "    constant %s as %q: %s\n", macro.hexName, macro.cName, macro.hexal)
		exported = append(exported, macro.hexName)
	}
	for _, function := range importer.functions {
		if !function.ok {
			continue
		}
		name := function.hexName
		if function.hexName != function.cName {
			name += " as " + fmt.Sprintf("%q", function.cName)
		}
		parameters := make([]string, 0, len(function.parameters))
		for _, parameter := range function.parameters {
			entry := parameter.hexName + ": " + parameter.hexal
			if parameter.spelling != "" {
				entry += " as " + fmt.Sprintf("%q", parameter.spelling)
			}
			parameters = append(parameters, entry)
		}
		if function.noResult {
			fmt.Fprintf(&body, "    fun %s(%s)\n", name, strings.Join(parameters, ", "))
		} else {
			result := function.result
			if function.resultSpelling != "" {
				result += " as " + fmt.Sprintf("%q", function.resultSpelling)
			}
			fmt.Fprintf(&body, "    fun %s(%s): %s\n", name, strings.Join(parameters, ", "), result)
		}
		exported = append(exported, function.hexName)
	}
	for _, global := range importer.globals {
		if !global.ok {
			continue
		}
		name := global.hexName
		if global.hexName != global.cName {
			name += " as " + fmt.Sprintf("%q", global.cName)
		}
		qualifier := ""
		if global.mutable {
			qualifier = "mut "
		}
		fmt.Fprintf(&body, "    global %s%s: %s\n", qualifier, name, global.hexal)
		exported = append(exported, global.hexName)
	}
	body.WriteString("end\n")

	if len(exported) > 0 {
		exported = dedupeByName(exported, func(name string) string { return name })
		slices.Sort(exported)
		body.WriteString("\nexport\n")
		for _, name := range exported {
			fmt.Fprintf(&body, "    %s,\n", name)
		}
		body.WriteString("end\n")
	}
	return body.String()
}

// coalesceRedeclarations collapses repeated compatible declarations so each
// exact C identity produces one Hexal declaration. An incompatible
// redeclaration is not selected by traversal order: the first occurrence wins
// and the rest are dropped.
func (importer *importer) coalesceRedeclarations() {
	slices.SortStableFunc(importer.typedefs, func(left, right *typedefRecord) int {
		return declarationOrder(left.line, left.cName, right.line, right.cName)
	})
	slices.SortStableFunc(importer.records, func(left, right *recordRecord) int {
		return declarationOrder(left.line, left.tag+" "+left.cName, right.line, right.tag+" "+right.cName)
	})
	slices.SortStableFunc(importer.enums, func(left, right *enumRecord) int {
		return declarationOrder(left.line, left.cName, right.line, right.cName)
	})
	slices.SortStableFunc(importer.functions, func(left, right *functionRecord) int {
		return declarationOrder(left.line, left.cName, right.line, right.cName)
	})
	slices.SortStableFunc(importer.globals, func(left, right *globalRecord) int {
		return declarationOrder(left.line, left.cName, right.line, right.cName)
	})
	importer.typedefs = dedupeByName(importer.typedefs, func(record *typedefRecord) string { return record.cName })
	importer.records = dedupeByName(importer.records, func(record *recordRecord) string { return record.tag + " " + record.cName })
	importer.functions = dedupeByName(importer.functions, func(record *functionRecord) string { return record.cName })
	importer.globals = dedupeByName(importer.globals, func(record *globalRecord) string { return record.cName })
	enums := make([]*enumRecord, 0, len(importer.enums))
	seenEnums := make(map[string]bool)
	for index, record := range importer.enums {
		key := record.cName
		if key == "" {
			key = fmt.Sprintf("#anonymous-%d", index)
		}
		if seenEnums[key] {
			continue
		}
		seenEnums[key] = true
		record.enumerators = dedupeByName(record.enumerators, func(constant namedConstant) string { return constant.cName })
		enums = append(enums, record)
	}
	importer.enums = enums
}

// declarationOrder orders two otherwise independent declarations by source
// location, then exact C name.
func declarationOrder(leftLine int, leftName string, rightLine int, rightName string) int {
	if leftLine != rightLine {
		return cmp.Compare(leftLine, rightLine)
	}
	return strings.Compare(leftName, rightName)
}

// dedupeByName keeps the first item for each key, preserving order.
func dedupeByName[T any](items []T, key func(T) string) []T {
	seen := make(map[string]bool, len(items))
	result := make([]T, 0, len(items))
	for _, item := range items {
		k := key(item)
		if seen[k] {
			continue
		}
		seen[k] = true
		result = append(result, item)
	}
	return result
}
