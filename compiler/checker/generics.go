package checker

import (
	"fmt"
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// genericTable is the compilation-scoped registry of open generic templates
// and concrete specializations. It lives on the module scope and is shared by
// every child scope, so one compilation observes one set of specializations.
type genericTable struct {
	types     map[string]*openGenericType
	functions map[string]*openGenericFunction
	methods   map[string]*openGenericMethod

	active map[string]bool               // specialization keys being resolved
	frame  map[string]compilerTypes.Type // current parameter frame; nil outside generic resolution
	open   bool                          // true while open-checking a generic body

	aliasSpecializations    map[string]compilerTypes.Type
	objectSpecializations   map[string]compilerTypes.Type
	adtSpecializations      map[string]compilerTypes.Type
	objectOpen              map[*compilerTypes.ObjectType]*openGenericType
	objectArguments         map[*compilerTypes.ObjectType][]compilerTypes.Type
	adtOpen                 map[*compilerTypes.AdtType]*openGenericType
	adtArguments            map[*compilerTypes.AdtType][]compilerTypes.Type
	typeDeclarations        []TypeDeclaration
	functionSpecializations map[string]FunctionDeclaration
	methodSpecializations   map[string]MethodDeclaration

	// registry and moduleID name the enclosing module's import graph: a
	// QualifiedTypeExpression resolves through them. moduleScope installs
	// both; the table is per module, so the pair is stable for the whole
	// compilation.
	registry *ModuleRegistry
	moduleID string
}

func newGenericTable() *genericTable {
	return &genericTable{
		types:                   make(map[string]*openGenericType),
		functions:               make(map[string]*openGenericFunction),
		methods:                 make(map[string]*openGenericMethod),
		active:                  make(map[string]bool),
		aliasSpecializations:    make(map[string]compilerTypes.Type),
		objectSpecializations:   make(map[string]compilerTypes.Type),
		adtSpecializations:      make(map[string]compilerTypes.Type),
		objectOpen:              make(map[*compilerTypes.ObjectType]*openGenericType),
		objectArguments:         make(map[*compilerTypes.ObjectType][]compilerTypes.Type),
		adtOpen:                 make(map[*compilerTypes.AdtType]*openGenericType),
		adtArguments:            make(map[*compilerTypes.AdtType][]compilerTypes.Type),
		functionSpecializations: make(map[string]FunctionDeclaration),
		methodSpecializations:   make(map[string]MethodDeclaration),
	}
}

// openGenericType is one generic type or alias declaration kept as an open
// template. Object targets are specialized into fresh nominal objects; plain
// type targets are resolved under a parameter frame and cached per argument
// list.
type openGenericType struct {
	Name        string
	Parameters  []lexer.Token
	Target      parser.TypeExpression
	Declaration *compilerTypes.GenericDeclaration
}

// openGenericFunction is one generic function declaration kept as an open
// template. Its body is re-checked under a concrete frame at specialization.
// A module-level template is identified by Name alone: one module has one
// name per declaration, so no collision is possible. Local is true for a
// generic anonymous function literal (openGenericLiteral), where identity
// instead carries the compiler-owned template identity: two literals can
// legitimately reuse the same synthesized name in disjoint scopes, and the
// module-wide type environment and specialization tables key on strings, so
// templateKey is what actually distinguishes them.
type openGenericFunction struct {
	Name        string
	Parameters  []lexer.Token
	Declaration parser.FunctionDeclaration
	Generic     *compilerTypes.GenericDeclaration
	identity    BindingID
	local       bool
}

// templateKey is the string that keys this template's type-environment
// registration, specialization cache, recursion guard, and generated C name
// stem. A module template keeps its bare source name, preserving today's
// generated names and cache keys exactly. A local template's key is
// qualified by its identity so two same-named local templates in disjoint
// scopes never share a cache entry, a type-parameter placeholder, or a
// generated symbol.
func (open *openGenericFunction) templateKey() string {
	if !open.local {
		return open.Name
	}
	return fmt.Sprintf("%s#%d", open.Name, open.identity)
}

// generatedStem is templateKey's counterpart for a generated C symbol
// fragment, which must be a valid C identifier and therefore cannot use
// templateKey's '#' separator. A module template keeps its bare name; a
// local template's stem is disambiguated with its identity so two local
// templates that reuse a name in disjoint scopes never share a symbol.
func (open *openGenericFunction) generatedStem() string {
	if !open.local {
		return open.Name
	}
	return fmt.Sprintf("%s_local%d", open.Name, open.identity)
}

// openGenericMethod is one generic method declaration. ReceiverParameters are
// the generic owner's parameters inherited from the receiver type.
type openGenericMethod struct {
	ObjectName         string
	Name               string
	ReceiverParameters []lexer.Token
	Parameters         []lexer.Token
	Declaration        parser.MethodDeclaration
	Object             *openGenericType
	Generic            *compilerTypes.GenericDeclaration
}

// specializeKey builds the deterministic in-compilation key for one
// specialization from the declaration name and the recursive module-qualified
// canonical keys of the arguments. Display names never participate: two
// same-named types from different modules produce different keys.
func specializeKey(name string, arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.CanonicalKey
	}
	return name + "|" + strings.Join(names, ",")
}

// specializeTypeName renders the deterministic source name of a specialized
// type, such as "Box<Int32>".
func specializeTypeName(name string, arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.Name
	}
	return name + "<" + strings.Join(names, ", ") + ">"
}

// specializeFunctionName renders the deterministic C-name stem of a
// specialized function, such as "identity_Int32".
func specializeFunctionName(name string, arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = compilerTypes.SanitizeIdentifier(argument.Name)
	}
	return name + "_" + strings.Join(names, "_")
}

func parameterFrame(parameters []lexer.Token, arguments []compilerTypes.Type) map[string]compilerTypes.Type {
	frame := make(map[string]compilerTypes.Type, len(parameters))
	for index, parameter := range parameters {
		if index < len(arguments) {
			frame[parameter.Lexeme] = arguments[index]
		}
	}
	return frame
}

// mergedFrame combines an enclosing generic frame with a nested template's
// own, so a generic literal specializing inside an already-specializing
// generic function or method can still resolve the enclosing type
// parameter. The active-name check at registration already rejects a
// nested declaration that redeclares an enclosing name, so no entry in
// inner ever legitimately overwrites one in outer.
func mergedFrame(outer, inner map[string]compilerTypes.Type) map[string]compilerTypes.Type {
	if len(outer) == 0 {
		return inner
	}
	merged := make(map[string]compilerTypes.Type, len(outer)+len(inner))
	for name, typ := range outer {
		merged[name] = typ
	}
	for name, typ := range inner {
		merged[name] = typ
	}
	return merged
}

// specializedFunctionList returns the cached concrete function specializations
// in deterministic specialization-key order for the generator. The final
// registry fold replaces this list with the defining module's full collection
// (own requests plus importers'), sorted identically.
func specializedFunctionList(generics *genericTable) []FunctionDeclaration {
	if generics == nil {
		return nil
	}
	return sortedFunctionSpecializations(generics.functionSpecializations, nil)
}

// specializedMethodList returns the cached concrete method specializations in
// deterministic specialization-key order for the generator.
func specializedMethodList(generics *genericTable) []MethodDeclaration {
	if generics == nil {
		return nil
	}
	return sortedMethodSpecializations(generics.methodSpecializations, nil)
}
