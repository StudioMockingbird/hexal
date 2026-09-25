package driver

// ctype.go owns C type spelling resolution: the scalar spelling tables,
// qualType parsing, and the resolution and Hexal spelling of parsed C
// types.

import (
	"strings"

	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// cSpellings maps one Hexal scalar's canonical C spelling.
var cSpellings = map[string]string{
	"Bool":    "bool",
	"Int8":    "int8_t",
	"Int16":   "int16_t",
	"Int32":   "int32_t",
	"Int64":   "int64_t",
	"UInt8":   "uint8_t",
	"UInt16":  "uint16_t",
	"UInt32":  "uint32_t",
	"UInt64":  "uint64_t",
	"Byte":    "uint8_t",
	"Float32": "float",
	"Float64": "double",
	"Size":    "size_t",
}

// scalarTarget selects the C data model the imported header normalizes under.
// The empty identity is unreachable in production, where an automatic import
// requires a qualified profile; it and every non-Linux identity keep the
// historical LLP64 selection so pure-Go normalization tests stay stable.
func (importer *importer) scalarTarget() specdata.TargetID {
	if importer.target == string(compilerTypes.TargetX86_64LinuxGNU) {
		return specdata.TargetLinuxGNU
	}
	return specdata.TargetWindowsUCRT
}

// scalarSpelling resolves one C base spelling to its Hexal scalar under the
// importer's target. The target-qualified registry owns the mapping, including
// the LP64/LLP64 long rule, so no driver-owned table duplicates it.
func (importer *importer) scalarSpelling(base string) (string, bool) {
	mapping, ok := specdata.CScalar(importer.scalarTarget(), base)
	if !ok {
		return "", false
	}
	return string(mapping.HexalType), true
}

// parsedCType is one parsed C type spelling.
type parsedCType struct {
	base        string
	pointer     int
	constLayer  bool
	unsupported bool
}

// parseCType parses one written C type spelling. Arrays, function pointers,
// and abstract declarator nesting are unsupported and omit their declaration.
func parseCType(qualType string) parsedCType {
	trimmed := strings.TrimSpace(qualType)
	if strings.ContainsAny(trimmed, "[]()") {
		return parsedCType{unsupported: true}
	}
	pointer := 0
	constLayer := false
	for {
		trimmed = strings.TrimSpace(trimmed)
		if strings.HasSuffix(trimmed, "*") {
			pointer++
			trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "*"))
			continue
		}
		break
	}
	words := strings.Fields(trimmed)
	kept := make([]string, 0, len(words))
	for _, word := range words {
		switch word {
		case "const":
			constLayer = true
		case "volatile", "restrict", "_Atomic", "__restrict", "__restrict__":
			return parsedCType{unsupported: true}
		default:
			kept = append(kept, word)
		}
	}
	if len(kept) == 0 {
		return parsedCType{unsupported: true}
	}
	return parsedCType{base: strings.Join(kept, " "), pointer: pointer, constLayer: constLayer}
}

// resolveType maps one written C type to its Hexal type and the `as` spelling
// it needs, or reports it unsupported.
func (importer *importer) resolveType(qualType string) (string, string, bool) {
	parsed := parseCType(qualType)
	if parsed.unsupported {
		return "", "", false
	}
	hexal, ok := importer.resolveParsed(parsed)
	if !ok {
		return "", "", false
	}
	return hexal, importer.asSpelling(hexal, qualType, parsed), true
}

// resolveParsed resolves one parsed C type to a Hexal type expression.
func (importer *importer) resolveParsed(parsed parsedCType) (string, bool) {
	if parsed.pointer == 0 {
		return importer.resolveBase(parsed.base)
	}
	element := ""
	if parsed.base == "void" {
		element = "Unknown"
	} else if parsed.base == "char" {
		element = "Byte"
	} else {
		base, ok := importer.resolveBase(parsed.base)
		if !ok {
			return "", false
		}
		element = base
	}
	hexal := "Ptr<" + element + ">"
	if !parsed.constLayer {
		hexal = "Ptr<mut " + element + ">"
	}
	return hexal + " | Nil", true
}

// resolveBase resolves one non-pointer base type to a Hexal type name.
func (importer *importer) resolveBase(base string) (string, bool) {
	if importer.typeUnsupported[base] {
		return "", false
	}
	// A declared typedef, record, or enum keeps its own name.
	if hexName, ok := importer.byCName[base]; ok {
		return hexName, true
	}
	if hexal, scalar := importer.scalarSpelling(base); scalar {
		return hexal, true
	}
	if strings.HasPrefix(base, "struct ") || strings.HasPrefix(base, "union ") {
		if hexName, ok := importer.byCName[base]; ok {
			return hexName, true
		}
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(base, "struct "), "union "))
		if hexName, ok := importer.byCName[name]; ok {
			return hexName, true
		}
		return "", false
	}
	if strings.HasPrefix(base, "enum ") {
		name := strings.TrimSpace(strings.TrimPrefix(base, "enum "))
		if hexName, ok := importer.byCName[name]; ok {
			return hexName, true
		}
		return "", false
	}
	if hexName, ok := importer.byCName[base]; ok {
		return hexName, true
	}
	return "", false
}

// asSpelling returns the exact C spelling a type position records, or an empty
// string when the Hexal type's own spelling already matches.
func (importer *importer) asSpelling(hexal, qualType string, parsed parsedCType) string {
	if parsed.pointer == 0 && importer.recordNames[hexal] {
		// A by-value record's tag and typedef spellings are one C type.
		return ""
	}
	written := strings.Join(strings.Fields(qualType), " ")
	if written == importer.ownSpelling(hexal) {
		return ""
	}
	return written
}

// ownSpelling renders the C spelling the generator emits for one Hexal type
// name, so a recorded `as` clause is emitted only when it differs.
func (importer *importer) ownSpelling(hexal string) string {
	if spelling, ok := cSpellings[hexal]; ok {
		return spelling
	}
	if hexal == "Unknown" {
		return "void"
	}
	if spelling, ok := importer.typeCSpelling[hexal]; ok {
		return spelling
	}
	if strings.HasPrefix(hexal, "Ptr<") && strings.HasSuffix(hexal, "> | Nil") {
		inner := hexal[len("Ptr<") : len(hexal)-len("> | Nil")]
		writable := false
		if strings.HasPrefix(inner, "mut ") {
			writable = true
			inner = inner[len("mut "):]
		}
		base := importer.ownSpelling(inner)
		if base == "" {
			return ""
		}
		if writable {
			return base + " *"
		}
		return "const " + base + " *"
	}
	return ""
}
