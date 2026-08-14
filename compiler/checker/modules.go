package checker

// Module identity, import registration, and the import-prefix rules of RFC
// 0034 Task 4, plus the exported-interface records of RFC 0034 Task 5.
// Resolution of import paths to files is the module phase's job (compile.go);
// this file assembles the alias -> canonical-id pairs every module scope
// carries while checking and the per-module exported records importers
// resolve against.

import (
	"sort"
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// ModuleRegistry records the import graph of one compilation: every reachable
// module's canonical identity, its import alias -> canonical target pairs,
// and the root module. It is assembled by CheckModules before any module is
// checked; the per-module exported records are published as each module
// checks clean, in dependency order, so an importer always sees the complete
// exported interface of its dependencies (RFC 0034 Task 5).
type ModuleRegistry struct {
	modules    map[string]*moduleEntry // canonical id -> entry
	order      []string                // canonical ids, dependencies first
	entrypoint string                  // canonical id of the root module
}

// moduleEntry is one module's import table and, once the module checks clean,
// its exported interface records: every exported function, type, and method
// resolved in that module's own type environment. The export set comes from
// the AST's export flags; the checked records are published by
// registerExports. RFC 0034 Task 6 adds the defining module's open generic
// templates (published by registerGenerics) and its specialization
// collection: the module's own requests plus every importer's, folded into
// its checked output by assembleSpecializations.
type moduleEntry struct {
	imports map[string]string // import alias -> canonical module id

	exports   map[string]bool                  // exported declaration names, from the AST (RFC 0034 Task 5)
	functions map[string]FunctionDeclaration   // exported functions by name
	types     map[string]compilerTypes.TypeUse // exported type names -> resolved use
	methods   map[string][]MethodDeclaration   // receiver type name -> exported methods

	// genericFunctions holds the module's exported generic function
	// templates. Importers resolve qualified generic calls through them and
	// record the specialization with the defining module (RFC 0034 Task 6).
	genericFunctions map[string]*openGenericFunction
	// functionSpecializations and methodSpecializations are the defining
	// module's specialization collections: its own requests, published by
	// registerGenerics, plus every importer's, keyed by specialization key
	// (declaration name, then canonical argument signature). The final pass
	// sorts the keys and emits the records into the module's checked output.
	functionSpecializations map[string]FunctionDeclaration
	methodSpecializations   map[string]MethodDeclaration
}

// buildModuleRegistry collects every reachable module's import aliases and
// export flags in dependency order. A module whose source key is absent is
// skipped exactly like CheckModules skips it. Paths are not resolved here: a
// payload that cannot canonicalize simply is kept as written, and resolution
// errors belong to the module phase.
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
		entry := &moduleEntry{
			imports: make(map[string]string),
			exports: make(map[string]bool),
		}
		// Imports are top-level items only and never statements, so a program
		// assembled from Statements alone has nothing to register.
		for _, item := range program.Items {
			switch item := item.(type) {
			case parser.ImportDeclaration:
				entry.imports[item.Alias.Lexeme] = canonicalModuleID(moduleID, strings.Trim(item.Path.Lexeme, "\""))
			case parser.TypeDeclaration:
				if item.Exported {
					entry.exports[item.Name.Lexeme] = true
				}
			case parser.FunctionDeclaration:
				if item.Exported {
					entry.exports[item.Name.Lexeme] = true
				}
			case parser.ImplDeclaration:
				if item.Exported {
					entry.exports[item.Name.Lexeme] = true
				}
			}
		}
		registry.modules[moduleID] = entry
	}
	return registry
}

// registerExports publishes one module's checked exported interface into the
// registry after the module checks clean. Only names marked exported by the
// AST are recorded; private declarations and specialization records (whose
// sanitized names never appear in the export set) never enter the tables, so
// a cross-module resolution either finds an exported name or reports it
// private.
func (registry *ModuleRegistry) registerExports(moduleID string, checked Program) {
	entry := registry.modules[moduleID]
	if entry == nil {
		return
	}
	entry.functions = make(map[string]FunctionDeclaration)
	entry.types = make(map[string]compilerTypes.TypeUse)
	entry.methods = make(map[string][]MethodDeclaration)
	for _, declaration := range checked.TypeDeclarations {
		// An open generic template carries no canonical type of its own and
		// is not recorded here; its specializations still close over the
		// template's exported name.
		if entry.exports[declaration.Name] && declaration.TypeUse.Type.Name != "" {
			entry.types[declaration.Name] = declaration.TypeUse
		}
	}
	for _, statement := range checked.Statements {
		switch declaration := statement.(type) {
		case FunctionDeclaration:
			if entry.exports[declaration.Name] {
				entry.functions[declaration.Name] = declaration
			}
		case MethodDeclaration:
			if entry.exports[declaration.Name] {
				entry.methods[declaration.Object.Name] = append(entry.methods[declaration.Object.Name], declaration)
			}
		}
	}
}

// exportedFunction resolves one exported function of the target module by
// name. The returned declaration carries the defining module's resolved
// signature; the caller checks arguments against its parameter uses.
func (registry *ModuleRegistry) exportedFunction(moduleID, name string) (FunctionDeclaration, bool) {
	entry, ok := registry.modules[moduleID]
	if !ok {
		return FunctionDeclaration{}, false
	}
	function, ok := entry.functions[name]
	return function, ok
}

// exportedType resolves one exported type of the target module by name.
func (registry *ModuleRegistry) exportedType(moduleID, name string) (compilerTypes.TypeUse, bool) {
	entry, ok := registry.modules[moduleID]
	if !ok {
		return compilerTypes.TypeUse{}, false
	}
	use, ok := entry.types[name]
	return use, ok
}

// registerGenerics publishes one module's open generic function templates and
// its own specialization requests into the registry after the module checks
// clean (RFC 0034 Task 6). Importers resolve qualified generic calls against
// the template records and record their requests beside the module's own, so
// the defining module's checked output carries every specialization of its
// declarations. Only exported templates are published; a private template is
// never reachable from an importer.
func (registry *ModuleRegistry) registerGenerics(moduleID string, generics *genericTable) {
	entry := registry.modules[moduleID]
	if entry == nil || generics == nil {
		return
	}
	entry.genericFunctions = make(map[string]*openGenericFunction, len(generics.functions))
	for name, open := range generics.functions {
		if entry.exports[name] {
			entry.genericFunctions[name] = open
		}
	}
	entry.functionSpecializations = make(map[string]FunctionDeclaration, len(generics.functionSpecializations))
	for key, declaration := range generics.functionSpecializations {
		entry.functionSpecializations[key] = declaration
	}
	entry.methodSpecializations = make(map[string]MethodDeclaration, len(generics.methodSpecializations))
	for key, declaration := range generics.methodSpecializations {
		entry.methodSpecializations[key] = declaration
	}
}

// genericFunction resolves one exported generic function template of the
// target module by name (RFC 0034 Task 6). Importers specialize it and
// record the result with the defining module.
func (registry *ModuleRegistry) genericFunction(moduleID, name string) (*openGenericFunction, bool) {
	entry, ok := registry.modules[moduleID]
	if !ok {
		return nil, false
	}
	open, ok := entry.genericFunctions[name]
	return open, ok
}

// specializationStore returns the defining module's specialization
// collection for generic functions. The map exists exactly when the module
// checked clean and published its templates, which is also the only time a
// template is reachable, so a found template always has a collection to
// record into.
func (registry *ModuleRegistry) specializationStore(moduleID string) map[string]FunctionDeclaration {
	entry, ok := registry.modules[moduleID]
	if !ok {
		return nil
	}
	return entry.functionSpecializations
}

// exportedMethod resolves one exported method of the target module by
// receiver type name and method name (RFC 0034 Task 6). Only exported
// methods are recorded, so a private method resolves nowhere and the caller
// reports the visibility failure at the call site.
func (registry *ModuleRegistry) exportedMethod(moduleID, objectName, name string) (MethodDeclaration, bool) {
	entry, ok := registry.modules[moduleID]
	if !ok {
		return MethodDeclaration{}, false
	}
	for _, method := range entry.methods[objectName] {
		if method.Name == name {
			return method, true
		}
	}
	return MethodDeclaration{}, false
}

// assembleSpecializations folds a defining module's specialization collection
// -- its own requests plus every importer's -- into its checked program
// (RFC 0034 Task 6). The records are deduplicated by specialization key and
// emitted in the deterministic order the generator consumes: declaration
// name, then the canonical argument signature string.
func (registry *ModuleRegistry) assembleSpecializations(moduleID string, program *Program) {
	entry := registry.modules[moduleID]
	if entry == nil {
		return
	}
	program.SpecializedFunctions = sortedFunctionSpecializations(entry.functionSpecializations)
	program.SpecializedMethods = sortedMethodSpecializations(entry.methodSpecializations)
}

// sortedFunctionSpecializations emits a generic-function specialization
// collection in specialization-key ascending order (declaration name first,
// then canonical argument signature string). The keys are the interner's
// specializeKey spellings, so sorting them is exactly the (decl, args) order
// the RFC requires.
func sortedFunctionSpecializations(collection map[string]FunctionDeclaration) []FunctionDeclaration {
	keys := make([]string, 0, len(collection))
	for key := range collection {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]FunctionDeclaration, 0, len(keys))
	for _, key := range keys {
		result = append(result, collection[key])
	}
	return result
}

// sortedMethodSpecializations is sortedFunctionSpecializations' counterpart
// for generic-method collections. The method keys carry the receiver object
// name and arguments before the method name and its own arguments, matching
// the order the interner builds them in.
func sortedMethodSpecializations(collection map[string]MethodDeclaration) []MethodDeclaration {
	keys := make([]string, 0, len(collection))
	for key := range collection {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]MethodDeclaration, 0, len(keys))
	for _, key := range keys {
		result = append(result, collection[key])
	}
	return result
}

// importTarget resolves one import alias of one module to its canonical
// target module id.
func (registry *ModuleRegistry) importTarget(moduleID, alias string) (string, bool) {
	if registry == nil {
		return "", false
	}
	entry, ok := registry.modules[moduleID]
	if !ok {
		return "", false
	}
	target, ok := entry.imports[alias]
	return target, ok
}

// findExportedADTVariant locates the exported ADT of the target module that
// carries variant, returning the ADT type and the variant record. Several
// exported ADTs may share a variant name; the scan is sorted by type name so
// the choice is deterministic.
func (registry *ModuleRegistry) findExportedADTVariant(moduleID, variant string) (compilerTypes.Type, *compilerTypes.AdtVariant, bool) {
	entry, ok := registry.modules[moduleID]
	if !ok {
		return compilerTypes.Type{}, nil, false
	}
	names := make([]string, 0, len(entry.types))
	for name := range entry.types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		typ := entry.types[name].Type
		if typ.Adt == nil {
			continue
		}
		for index := range typ.Adt.Variants {
			if typ.Adt.Variants[index].Name == variant {
				return typ, &typ.Adt.Variants[index], true
			}
		}
	}
	return compilerTypes.Type{}, nil, false
}

// privateToModuleDiagnostic is the RFC 0034 Task 5 visibility failure: a
// cross-module use of a name that is private or unknown in its target module.
func privateToModuleDiagnostic(token lexer.Token, name, target string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.NameError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  "declaration " + name + " is private to module " + target,
	}
}

// checkExportedClosure validates every exported declaration's source-level
// interface after its module checks clean: type aliases, function parameters
// and results, and method receivers, parameters, and results. Every nominal
// type (object or ADT) reachable from an exported interface must be a builtin
// or exported by its defining module; the first violation reports the
// exported declaration and the first private type. Only interfaces are
// validated: private types used inside exported generic bodies stay out of
// the walk until the module phase compiles those bodies, and interfaces
// contain no code.
func (registry *ModuleRegistry) checkExportedClosure(moduleID string, checked Program) compilerTypes.Diagnostics {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	entry := registry.modules[moduleID]
	if entry == nil {
		return diagnostics
	}
	// The cycle-breaking visited sets are per declaration: one exported
	// declaration's walk must not mark nominal types seen for a later one,
	// or a later declaration leaking the same private type would be silently
	// cleared instead of reported.
	for _, declaration := range checked.TypeDeclarations {
		if !entry.exports[declaration.Name] {
			continue
		}
		seenObjects := make(map[*compilerTypes.ObjectType]bool)
		seenADTs := make(map[*compilerTypes.AdtType]bool)
		if private := registry.privateTypeInUse(declaration.TypeUse.Type, seenObjects, seenADTs); private != "" {
			diagnostics = append(diagnostics, exportedClosureDiagnostic(declaration.Name, private, declaration.SourceLine, declaration.SourceColumn))
		}
	}
	for _, statement := range checked.Statements {
		seenObjects := make(map[*compilerTypes.ObjectType]bool)
		seenADTs := make(map[*compilerTypes.AdtType]bool)
		switch declaration := statement.(type) {
		case FunctionDeclaration:
			// The Fun<...> type carries the full parameter and result
			// interface in one signature.
			if !entry.exports[declaration.Name] {
				continue
			}
			if private := registry.privateTypeInUse(declaration.Type, seenObjects, seenADTs); private != "" {
				diagnostics = append(diagnostics, exportedClosureDiagnostic(declaration.Name, private, declaration.SourceLine, declaration.SourceColumn))
			}
		case MethodDeclaration:
			if !entry.exports[declaration.Name] {
				continue
			}
			private := registry.privateTypeInUse(declaration.SelfType, seenObjects, seenADTs)
			if private == "" {
				for _, parameter := range declaration.Parameters {
					if private = registry.privateTypeInUse(parameter.Type, seenObjects, seenADTs); private != "" {
						break
					}
				}
			}
			if private == "" && declaration.ResultUse != nil {
				private = registry.privateTypeInUse(declaration.ResultUse.Type, seenObjects, seenADTs)
			}
			if private != "" {
				diagnostics = append(diagnostics, exportedClosureDiagnostic(declaration.Name, private, declaration.SourceLine, declaration.SourceColumn))
			}
		}
	}
	return diagnostics
}

// exportedClosureDiagnostic is the RFC 0034 Task 5 interface-closure failure,
// located at the offending exported declaration.
func exportedClosureDiagnostic(name, private string, line, column int) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     line,
		Column:   column,
		Message:  "exported function " + name + " exposes private type " + private,
	}
}

// privateTypeInUse reports the name of the first private nominal type
// reachable from typ, or "" when the interface closes over only builtin and
// exported types. The visited sets break object and ADT cycles (self
// references behind pointers and alias loops).
func (registry *ModuleRegistry) privateTypeInUse(typ compilerTypes.Type, seenObjects map[*compilerTypes.ObjectType]bool, seenADTs map[*compilerTypes.AdtType]bool) string {
	switch {
	case typ.Object != nil:
		if seenObjects[typ.Object] {
			return ""
		}
		seenObjects[typ.Object] = true
		if !registry.nominalExported(typ) {
			return typ.Name
		}
		for _, member := range typ.Object.Members {
			if private := registry.privateTypeInUse(member.Type, seenObjects, seenADTs); private != "" {
				return private
			}
		}
		return ""
	case typ.Adt != nil:
		if seenADTs[typ.Adt] {
			return ""
		}
		seenADTs[typ.Adt] = true
		if !registry.nominalExported(typ) {
			return typ.Name
		}
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if private := registry.privateTypeInUse(member.Type, seenObjects, seenADTs); private != "" {
					return private
				}
			}
		}
		return ""
	case typ.Element != nil:
		return registry.privateTypeInUse(*typ.Element, seenObjects, seenADTs)
	case typ.NullableBase != nil:
		return registry.privateTypeInUse(*typ.NullableBase, seenObjects, seenADTs)
	case typ.Signature != nil:
		for _, parameter := range typ.Signature.Parameters {
			if private := registry.privateTypeInUse(parameter, seenObjects, seenADTs); private != "" {
				return private
			}
		}
		if typ.Signature.Result != nil {
			return registry.privateTypeInUse(*typ.Signature.Result, seenObjects, seenADTs)
		}
		return ""
	case typ.Union != nil:
		for _, member := range typ.Union.Members {
			if private := registry.privateTypeInUse(member, seenObjects, seenADTs); private != "" {
				return private
			}
		}
		return ""
	case typ.Array != nil:
		return registry.privateTypeInUse(typ.Array.Element, seenObjects, seenADTs)
	case typ.View != nil:
		return registry.privateTypeInUse(typ.View.Element, seenObjects, seenADTs)
	case typ.List != nil:
		return registry.privateTypeInUse(typ.List.Element, seenObjects, seenADTs)
	case typ.Dict != nil:
		if private := registry.privateTypeInUse(typ.Dict.Key, seenObjects, seenADTs); private != "" {
			return private
		}
		return registry.privateTypeInUse(typ.Dict.Value, seenObjects, seenADTs)
	case typ.Stream != nil:
		return registry.privateTypeInUse(typ.Stream.Element, seenObjects, seenADTs)
	case typ.Task != nil:
		return registry.privateTypeInUse(typ.Task.Result, seenObjects, seenADTs)
	case typ.Channel != nil:
		return registry.privateTypeInUse(typ.Channel.Element, seenObjects, seenADTs)
	case typ.Atomic != nil:
		return registry.privateTypeInUse(typ.Atomic.Element, seenObjects, seenADTs)
	default:
		// Scalars, Nil, Unknown, heap handles, and generic parameters are
		// never nominal and cannot be private.
		return ""
	}
}

// nominalExported reports whether a nominal type is visible to importers:
// either a builtin or a type some module in this compilation exports. The
// defining module is found by identity: every module resolves its nominal
// types in its own environment, so the ObjectType and AdtType pointers are
// unique to the defining module and its exported records. A specialized
// generic's name ("Box<Int32>") matches its template's exported name ("Box")
// after the argument list is stripped.
func (registry *ModuleRegistry) nominalExported(typ compilerTypes.Type) bool {
	if _, builtin := compilerTypes.Lookup(typ.Name); builtin {
		return true
	}
	base := typ.Name
	if angle := strings.Index(base, "<"); angle >= 0 {
		base = base[:angle]
	}
	for _, entry := range registry.modules {
		if entry.exports[base] {
			return true
		}
		for name := range entry.types {
			use := entry.types[name]
			if typ.Object != nil && use.Type.Object == typ.Object {
				return true
			}
			if typ.Adt != nil && use.Type.Adt == typ.Adt {
				return true
			}
		}
	}
	return false
}

// canonicalModuleID resolves one module-path payload (the raw string between
// the quotes) relative to the importing module and returns the target's
// canonical identity. It mirrors the lexical resolution in
// compiler/compile.go's resolveImportPath: "." components drop, ".." pops the
// importing module's directory (never above the logical root), and a trailing
// ".hex" is spelling, not identity. compile.go is the authority for validity
// -- it runs before checking and rejects non-relative paths, escapes above
// the logical root, invalid components, and missing modules -- so the checker
// only re-derives the identity for registry keys; a payload that reached
// checking always yields the same canonical id as the resolver. A payload
// that never resolved (direct checker callers) falls back to the spelling
// with ".hex" stripped.
func canonicalModuleID(fromModule, payload string) string {
	path := payload
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		path = path[1 : len(path)-1]
	}
	if !strings.HasPrefix(path, "./") && !strings.HasPrefix(path, "../") {
		return strings.TrimSuffix(path, ".hex")
	}
	dir := ""
	if slash := strings.LastIndex(fromModule, "/"); slash >= 0 {
		dir = fromModule[:slash]
	}
	rest := path
	for strings.HasPrefix(rest, "../") {
		if slash := strings.LastIndex(dir, "/"); slash >= 0 {
			dir = dir[:slash]
		} else {
			dir = ""
		}
		rest = rest[3:]
	}
	if strings.HasPrefix(rest, "./") {
		rest = rest[2:]
	}
	rest = strings.TrimSuffix(rest, ".hex")
	if dir != "" {
		rest = dir + "/" + rest
	}
	return rest
}
