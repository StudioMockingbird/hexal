package driver

// Selection and normalization: decode the Clang JSON AST into driver-private
// records, select the declarations the foreign binding model can represent,
// order their type dependencies, and emit one ordinary `extern c` binding
// module. Nothing here is exposed through a compiler API: the result is an
// ordinary Hexal source string the unchanged compiler parses, checks, and
// lowers.

import (
	"encoding/json"
	"fmt"
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

// normalizeHeader decodes one frontend JSON AST and produces the binding
// module. A malformed document fails closed.
func normalizeHeader(text string, index *lineIndex, request compiler.CImportRequest, options headerOptions) (string, *BuildError) {
	var root astNode
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		return "", &BuildError{Stage: StageCompile, Message: "C header frontend inspection produced malformed JSON"}
	}
	if root.Kind != kindTranslationUnit {
		return "", &BuildError{Stage: StageCompile, Message: "C header frontend inspection produced no translation unit"}
	}
	importer := newImporter(request, index, options.target, options.macroTypes)
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
	// target is the selected Hexal target-profile identity. It selects the C
	// data model fundamental spellings resolve under: specdata owns the
	// target-qualified scalar mappings and this identity picks the record set.
	target string
	// macroTypes maps one object-like macro defined in the requested header to
	// the C qualType Clang proved for it. A macro whose type maps to a
	// supported foreign constant (a scalar, a data pointer, or a complete
	// foreign record) is emitted; every other macro is omitted whole, exactly
	// like an unsupported declaration.
	macroTypes map[string]string
}

type typedefRecord struct {
	cName      string
	hexName    string
	underlying string
	hexal      string
	ok         bool
	line       int
	// inline marks a typedef the foreign binding model cannot express as a transparent alias
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

func newImporter(request compiler.CImportRequest, index *lineIndex, target string, macroTypes map[string]string) *importer {
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
		target:            target,
		macroTypes:        macroTypes,
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

// assignName assigns the Hexal name for one exact C name under deterministic
// rules: a legal, non-protected, unambiguous name is kept;
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
