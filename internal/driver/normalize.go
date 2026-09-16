package driver

// RFC 0193 selection and normalization: decode the Clang JSON AST into
// driver-private records, select the declarations RFC 0039 can represent,
// order their type dependencies, and emit one ordinary `extern c` binding
// module. Nothing here is exposed through a compiler API: the result is an
// ordinary Hexal source string the unchanged compiler parses, checks, and
// lowers.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

// The decoder recognizes exactly the frontend node kinds this initial subset
// needs. Any other top-level node is ignored; an unknown shape on the
// dependency path of a selected declaration makes that declaration
// unsupported.
const (
	kindTranslationUnit = "TranslationUnitDecl"
	kindTypedef         = "TypedefDecl"
	kindRecord          = "RecordDecl"
	kindField           = "FieldDecl"
	kindEnum            = "EnumDecl"
	kindEnumConstant    = "EnumConstantDecl"
	kindFunction        = "FunctionDecl"
	kindParm            = "ParmVarDecl"
	kindVar             = "VarDecl"
)

// astNode is one frontend JSON AST node. Only the fields the initial subset
// reads are decoded; every other JSON field is ignored.
type astNode struct {
	ID                 string      `json:"id"`
	Kind               string      `json:"kind"`
	Name               string      `json:"name"`
	Loc                astLocation `json:"loc"`
	Type               astType     `json:"type"`
	Inner              []astNode   `json:"inner"`
	IsImplicit         bool        `json:"isImplicit"`
	StorageClass       string      `json:"storageClass"`
	Inline             bool        `json:"inline"`
	Variadic           bool        `json:"variadic"`
	CompleteDefinition bool        `json:"completeDefinition"`
	TagUsed            string      `json:"tagUsed"`
	IsReferenced       bool        `json:"isReferenced"`
}

// astLocation is one source location.
type astLocation struct {
	Offset       int          `json:"offset"`
	Line         int          `json:"line"`
	File         string       `json:"file"`
	PresumedFile string       `json:"presumedFile"`
	PresumedLine int          `json:"presumedLine"`
	Col          int          `json:"col"`
	IncludedFrom *astLocation `json:"includedFrom"`
	ExpansionLoc *astLocation `json:"expansionLoc"`
	SpellingLoc  *astLocation `json:"spellingLoc"`
}

// astType carries one written C type spelling.
type astType struct {
	QualType          string `json:"qualType"`
	DesugaredQualType string `json:"desugaredQualType"`
}

// originFile resolves the physical file a node came from, preferring the
// presumed spelling that line markers restore.
func (location astLocation) originFile() string {
	for _, candidate := range []string{location.PresumedFile, location.File} {
		if candidate != "" {
			return candidate
		}
	}
	for _, nested := range []*astLocation{location.ExpansionLoc, location.SpellingLoc} {
		if nested != nil {
			if file := nested.originFile(); file != "" {
				return file
			}
		}
	}
	if location.IncludedFrom != nil {
		return location.IncludedFrom.originFile()
	}
	return ""
}

// normalizeHeader decodes one frontend JSON AST and produces the RFC 0039
// binding module. A malformed document fails closed.
func normalizeHeader(text string, index *lineIndex, request compiler.CImportRequest, options headerOptions) (string, *BuildError) {
	var root astNode
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		return "", &BuildError{Stage: StageCompile, Message: "C header frontend inspection produced malformed JSON"}
	}
	if root.Kind != kindTranslationUnit {
		return "", &BuildError{Stage: StageCompile, Message: "C header frontend inspection produced no translation unit"}
	}
	importer := newImporter(request, index)
	importer.collect(root.Inner)
	return importer.emit(), nil
}

// importer accumulates the selected declarations and assigns Hexal names.
type importer struct {
	request   compiler.CImportRequest
	typedefs  []*typedefRecord
	records   []*recordRecord
	enums     []*enumRecord
	functions []*functionRecord
	globals   []*globalRecord
	// byCName maps an exact C type name to its assigned Hexal spelling.
	byCName map[string]string
	// used is every Hexal name already taken.
	used map[string]bool
	// index resolves a preprocessed line to its physical file and line.
	index *lineIndex
	// typeCSpelling maps an assigned Hexal type name to its C spelling.
	typeCSpelling map[string]string
	// recordNames marks the Hexal names that are records.
	recordNames map[string]bool
	// coalesced marks typedefs whose record declaration serves both.
	coalesced map[string]bool
	// typeNames is every assigned Hexal type name, so an ordering pass can
	// find one declaration's references in its resolved type spellings.
	typeNames map[string]bool
	// typeUnsupported marks exact C type names whose declaration is not
	// representable, so a declaration that depends on one is omitted whole
	// rather than emitting an unresolved reference.
	typeUnsupported map[string]bool
	// incompleteRecords marks the Hexal names of opaque records: a transparent
	// alias to one is not a value type, so it resolves inline instead.
	incompleteRecords map[string]bool
}

type typedefRecord struct {
	cName      string
	hexName    string
	underlying string
	hexal      string
	ok         bool
	line       int
	// inline marks a typedef RFC 0039 cannot express as a transparent alias
	// (a nullable pointer spelling): its name resolves to the underlying Hexal
	// type at every use site and no declaration is emitted.
	inline bool
}

type fieldRecord struct {
	cName    string
	hexName  string
	written  string
	hexal    string
	spelling string
	mutable  bool
}

type recordRecord struct {
	tag      string
	cName    string
	hexName  string
	spelling string
	complete bool
	fields   []fieldRecord
	ok       bool
	line     int
}

type enumRecord struct {
	cName       string
	hexName     string
	enumerators []namedConstant
	ok          bool
	line        int
}

type namedConstant struct {
	cName   string
	hexName string
}

type parameterRecord struct {
	cName    string
	hexName  string
	written  string
	hexal    string
	spelling string
	ok       bool
}

type functionRecord struct {
	cName          string
	hexName        string
	parameters     []parameterRecord
	resultWritten  string
	result         string
	resultSpelling string
	noResult       bool
	ok             bool
	line           int
}

type globalRecord struct {
	cName    string
	hexName  string
	written  string
	hexal    string
	spelling string
	mutable  bool
	ok       bool
	line     int
}

func newImporter(request compiler.CImportRequest, index *lineIndex) *importer {
	return &importer{
		request:           request,
		byCName:           make(map[string]string),
		used:              make(map[string]bool),
		index:             index,
		typeCSpelling:     make(map[string]string),
		recordNames:       make(map[string]bool),
		coalesced:         make(map[string]bool),
		typeNames:         make(map[string]bool),
		typeUnsupported:   make(map[string]bool),
		incompleteRecords: make(map[string]bool),
	}
}

// origin resolves one node's physical file and line through the preprocessed
// line-marker index, falling back to the location's own spelling.
func (importer *importer) origin(node *astNode) (string, int) {
	if importer.index != nil && node.Loc.Line > 0 {
		if file, line := importer.index.origin(node.Loc.Line); file != "" {
			return file, line
		}
	}
	return node.Loc.originFile(), node.Loc.PresumedLine
}

// collect walks the translation unit's direct declarations in source order.
func (importer *importer) collect(nodes []astNode) {
	for index := range nodes {
		node := &nodes[index]
		if node.IsImplicit {
			continue
		}
		file, _ := importer.origin(node)
		if isBuiltinOrigin(file) {
			continue
		}
		switch node.Kind {
		case kindTypedef:
			importer.collectTypedef(node)
		case kindRecord:
			importer.collectRecord(node)
		case kindEnum:
			importer.collectEnum(node)
		case kindFunction:
			importer.collectFunction(node)
		case kindVar:
			importer.collectGlobal(node)
		}
	}
}

func (importer *importer) collectTypedef(node *astNode) {
	if node.Name == "" {
		return
	}
	importer.typedefs = append(importer.typedefs, &typedefRecord{cName: node.Name, underlying: node.Type.QualType, line: node.Loc.Line})
}

func (importer *importer) collectRecord(node *astNode) {
	if node.Name == "" {
		return
	}
	tag := node.TagUsed
	if tag == "" {
		tag = "struct"
	}
	record := &recordRecord{
		tag:      tag,
		cName:    node.Name,
		complete: node.CompleteDefinition,
		line:     node.Loc.Line,
	}
	for index := range node.Inner {
		field := &node.Inner[index]
		if field.Kind != kindField || field.Name == "" {
			continue
		}
		record.fields = append(record.fields, fieldRecord{cName: field.Name, written: field.Type.QualType})
	}
	importer.records = append(importer.records, record)
}

func (importer *importer) collectEnum(node *astNode) {
	record := &enumRecord{cName: node.Name, line: node.Loc.Line}
	for index := range node.Inner {
		entry := &node.Inner[index]
		if entry.Kind != kindEnumConstant || entry.Name == "" {
			continue
		}
		record.enumerators = append(record.enumerators, namedConstant{cName: entry.Name})
	}
	importer.enums = append(importer.enums, record)
}

func (importer *importer) collectFunction(node *astNode) {
	if node.Name == "" || node.Variadic {
		return
	}
	// Only external-linkage declarations and included `static inline`
	// definitions are retained.
	if node.StorageClass == "static" && !node.Inline {
		return
	}
	record := &functionRecord{cName: node.Name, line: node.Loc.Line}
	record.resultWritten, record.noResult = splitFunctionResult(node.Type.QualType)
	for index := range node.Inner {
		parameter := &node.Inner[index]
		if parameter.Kind != kindParm {
			continue
		}
		record.parameters = append(record.parameters, parameterRecord{cName: parameter.Name, written: parameter.Type.QualType})
	}
	importer.functions = append(importer.functions, record)
}

func (importer *importer) collectGlobal(node *astNode) {
	if node.Name == "" {
		return
	}
	if node.StorageClass != "extern" {
		return
	}
	mutable := !hasConstQualifier(node.Type.QualType)
	importer.globals = append(importer.globals, &globalRecord{cName: node.Name, written: node.Type.QualType, mutable: mutable, line: node.Loc.Line})
}

// splitFunctionResult splits one function `qualType` into its result spelling
// and whether the function returns nothing. The parameter list is the last
// top-level parenthesized group; a nested declarator makes the declaration
// unsupported later.
func splitFunctionResult(qualType string) (string, bool) {
	open := strings.LastIndex(qualType, "(")
	if open < 0 {
		return "", false
	}
	result := strings.TrimSpace(qualType[:open])
	if result == "void" {
		return "", true
	}
	return result, false
}

func hasConstQualifier(qualType string) bool {
	for _, token := range strings.Fields(qualType) {
		if token == "const" {
			return true
		}
	}
	return false
}

// isBuiltinOrigin reports whether a location names a compiler pseudo-file or
// has no file at all, which is how a builtin-only declaration appears.
func isBuiltinOrigin(file string) bool {
	if file == "" {
		return true
	}
	return strings.HasPrefix(file, "<")
}

// assignName assigns the Hexal name for one exact C name under RFC 0039's
// deterministic rules: a legal, non-protected, unambiguous name is kept;
// otherwise the name is escaped with the compiler-owned prefix, and an escaped
// base that still collides receives a numeric suffix. Every exact name is
// reserved before any escaped name.
func (importer *importer) assignName(cName string) string {
	if hexName, ok := importer.byCName[cName]; ok {
		return hexName
	}
	hexName := cName
	if !usableHexalName(cName) || importer.used[hexName] {
		hexName = importer.escape(cName)
	}
	importer.used[hexName] = true
	importer.byCName[cName] = hexName
	return hexName
}

// escape derives one deterministic escaped Hexal name for a C name.
func (importer *importer) escape(cName string) string {
	base := "hex_cvar_" + cName
	candidate := base
	for suffix := 0; importer.used[candidate]; suffix++ {
		candidate = fmt.Sprintf("%s_%d", base, suffix)
	}
	return candidate
}

// usableHexalName reports whether one exact C name is a legal, non-protected,
// unambiguous Hexal identifier.
func usableHexalName(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "_") {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		letter := character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		if index == 0 {
			if !letter {
				return false
			}
			continue
		}
		if !letter && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return !hexalKeyword(name) && !compilerTypes.IsProtectedTypeName(name)
}

// hexalKeyword lists the words a Hexal identifier may not be.
func hexalKeyword(name string) bool {
	switch name {
	case "type", "fun", "method", "match", "if", "then", "else", "while", "for", "in", "end",
		"return", "break", "continue", "defer", "errdefer", "unsafe", "import", "export",
		"static", "mut", "is", "as", "do", "true", "false", "nil", "self", "spawn", "try":
		return true
	default:
		return false
	}
}

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

// exactWidthTypedef maps one C exact-width or platform typedef to its Hexal
// canonical scalar.
var exactWidthTypedef = map[string]string{
	"int8_t": "Int8", "int16_t": "Int16", "int32_t": "Int32", "int64_t": "Int64",
	"uint8_t": "UInt8", "uint16_t": "UInt16", "uint32_t": "UInt32", "uint64_t": "UInt64",
	"size_t": "Size", "bool": "Bool",
}

// fundamentalSpelling maps one C fundamental type spelling to its Hexal
// scalar. `long` is 32-bit on the LLP64 Windows profile.
func fundamentalSpelling(base string) (string, bool) {
	switch base {
	case "_Bool", "bool":
		return "Bool", true
	case "char", "signed char":
		return "Int8", true
	case "unsigned char":
		return "UInt8", true
	case "short", "short int", "signed short", "signed short int":
		return "Int16", true
	case "unsigned short", "unsigned short int":
		return "UInt16", true
	case "int", "signed", "signed int":
		return "Int32", true
	case "unsigned", "unsigned int":
		return "UInt32", true
	case "long", "long int", "signed long", "signed long int":
		return "Int32", true
	case "unsigned long", "unsigned long int":
		return "UInt32", true
	case "long long", "long long int", "signed long long", "signed long long int":
		return "Int64", true
	case "unsigned long long", "unsigned long long int":
		return "UInt64", true
	case "float":
		return "Float32", true
	case "double":
		return "Float64", true
	default:
		return "", false
	}
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
	if hexal, exact := exactWidthTypedef[base]; exact {
		return hexal, true
	}
	if hexal, scalar := fundamentalSpelling(base); scalar {
		if base == "void" {
			return "", false
		}
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

	// Required type dependencies precede their users: records (each field's
	// record first), then typedefs (each referenced type first), then the
	// declarations that use them.
	for _, record := range importer.orderedRecords() {
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
			// A record field records no independent C type spelling: RFC 0039's
			// extern-member `as` names the C field, and the field's C
			// representation follows from its Hexal type.
			fmt.Fprintf(&body, "        %s%s: %s,\n", qualifier, field.hexName, field.hexal)
		}
		body.WriteString("    end\n")
		exported = append(exported, record.hexName)
	}
	for _, record := range importer.orderedTypedefs() {
		fmt.Fprintf(&body, "    type %s is %s\n", record.hexName, record.hexal)
		exported = append(exported, record.hexName)
	}
	for _, enum := range importer.enums {
		if !enum.ok {
			continue
		}
		if enum.cName != "" {
			fmt.Fprintf(&body, "    type %s is Int32\n", enum.hexName)
			exported = append(exported, enum.hexName)
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
		sort.Strings(exported)
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
	sort.SliceStable(importer.typedefs, func(i, j int) bool {
		return declarationOrder(importer.typedefs[i].line, importer.typedefs[i].cName, importer.typedefs[j].line, importer.typedefs[j].cName)
	})
	sort.SliceStable(importer.records, func(i, j int) bool {
		return declarationOrder(importer.records[i].line, importer.records[i].tag+" "+importer.records[i].cName, importer.records[j].line, importer.records[j].tag+" "+importer.records[j].cName)
	})
	sort.SliceStable(importer.enums, func(i, j int) bool {
		return declarationOrder(importer.enums[i].line, importer.enums[i].cName, importer.enums[j].line, importer.enums[j].cName)
	})
	sort.SliceStable(importer.functions, func(i, j int) bool {
		return declarationOrder(importer.functions[i].line, importer.functions[i].cName, importer.functions[j].line, importer.functions[j].cName)
	})
	sort.SliceStable(importer.globals, func(i, j int) bool {
		return declarationOrder(importer.globals[i].line, importer.globals[i].cName, importer.globals[j].line, importer.globals[j].cName)
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
func declarationOrder(leftLine int, leftName string, rightLine int, rightName string) bool {
	if leftLine != rightLine {
		return leftLine < rightLine
	}
	return leftName < rightName
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
			record.hexal = hexal
			record.ok = true
			if strings.Contains(hexal, "|") || importer.incompleteRecords[hexal] {
				// RFC 0039 has no transparent alias for a nullable pointer or
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

// orderedRecords returns the emitted records ordered so that every record a
// field names appears first.
func (importer *importer) orderedRecords() []*recordRecord {
	items := make([]*recordRecord, 0, len(importer.records))
	for _, record := range importer.records {
		if record.ok {
			items = append(items, record)
		}
	}
	return topoSort(items,
		func(record *recordRecord) string { return record.hexName },
		func(record *recordRecord) []string {
			dependencies := make([]string, 0)
			for _, field := range record.fields {
				dependencies = append(dependencies, importer.referencedNames(field.hexal)...)
			}
			return dependencies
		})
}

// orderedTypedefs returns the emitted typedefs deduplicated by Hexal name and
// ordered so that every type a typedef names appears first.
func (importer *importer) orderedTypedefs() []*typedefRecord {
	seen := make(map[string]bool)
	items := make([]*typedefRecord, 0, len(importer.typedefs))
	for _, record := range importer.typedefs {
		if !record.ok || record.inline || importer.coalesced[record.cName] || seen[record.hexName] {
			continue
		}
		seen[record.hexName] = true
		items = append(items, record)
	}
	return topoSort(items,
		func(record *typedefRecord) string { return record.hexName },
		func(record *typedefRecord) []string { return importer.referencedNames(record.hexal) })
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
