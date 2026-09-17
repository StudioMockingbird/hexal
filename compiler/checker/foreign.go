package checker

// Handwritten C interoperability: `extern c from <header> do ... end`. A
// foreign block declares the exact C symbols and the checked Hexal types of one
// requested header. It is the one binding form: an automatic import prepares
// the same ordinary source, so the checker has no separate automatic-binding
// representation or trusted fast path.
//
// The checked tree keeps two different facts for every foreign declaration:
// the Hexal type used for source checking, and the exact C spelling used at the
// ABI boundary. The generator uses the C spelling only at foreign positions;
// ordinary Hexal storage keeps its own representation.

import (
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// ForeignHeader identifies one required C header by its payload and form.
// System and quoted forms are distinct identities.
type ForeignHeader struct {
	Payload string
	System  bool
}

// ForeignFunctionDeclaration is one checked handwritten foreign function. It
// has no body: the included header supplies the definition. Type is the
// Fun<...> type its name produces in a value position; Result is nil when the
// C function returns no value.
type ForeignFunctionDeclaration struct {
	Name         string
	CName        string
	Header       ForeignHeader
	Parameters   []FunctionParameter
	Result       *compilerTypes.Type
	ResultUse    *compilerTypes.TypeUse
	ResultCName  string
	Type         compilerTypes.Type
	SourceLine   int
	SourceColumn int
	Exported     bool
}

// ForeignConstantDeclaration is one checked foreign constant: a typed,
// non-addressable C expression whose spelling is an enumerator or object-like
// macro identifier. Its type is a scalar, a data pointer, or a complete
// foreign record, and it is readable without unsafe.
type ForeignConstantDeclaration struct {
	Name         string
	CName        string
	Header       ForeignHeader
	Type         compilerTypes.Type
	TypeUse      compilerTypes.TypeUse
	SourceLine   int
	SourceColumn int
	Exported     bool
}

// ForeignGlobalDeclaration is one checked foreign global. Mutable renders reads
// and writes; a fixed global permits reads only. Every access requires unsafe.
type ForeignGlobalDeclaration struct {
	Name         string
	CName        string
	Header       ForeignHeader
	Type         compilerTypes.Type
	TypeUse      compilerTypes.TypeUse
	Mutable      bool
	SourceLine   int
	SourceColumn int
	Exported     bool
}

// ForeignRecordDeclaration is one declared foreign C record: complete or
// opaque. The record's Type carries the program-wide target-qualified
// identity, so the same C record reached through two headers is one Hexal
// type.
type ForeignRecordDeclaration struct {
	Name         string
	CName        string
	Header       ForeignHeader
	Type         compilerTypes.Type
	TypeUse      compilerTypes.TypeUse
	SourceLine   int
	SourceColumn int
	Exported     bool
}

// checkForeignDeclarations checks every leading foreign block and publishes the
// declarations into the module's value scope and type environment. It runs
// before pass 1 so an ordinary declaration that reuses a foreign name reports
// the ordinary duplicate-name diagnostic. A block whose declaration cannot be
// represented fails closed; no partial declaration is published.
func checkForeignDeclarations(blocks []parser.ExternBlock, ctx checkContext, checked *Program, target compilerTypes.TargetProfileID, moduleID string) compilerTypes.Diagnostics {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	if len(blocks) == 0 {
		return diagnostics
	}
	if target == "" {
		// Every foreign ABI fact is target-dependent, so an unqualified target
		// cannot be checked. Report once, at the first block.
		diagnostics = append(diagnostics, configurationErrorAt(blocks[0].Keyword, "C interoperability requires a qualified target"))
		return diagnostics
	}
	seenHeaders := make(map[ForeignHeader]bool)
	cSymbols := make(map[string]string) // exact C symbol -> declaring form
	for _, block := range blocks {
		header := ForeignHeader{Payload: block.Header.CHeader, System: block.Header.System}
		if !seenHeaders[header] {
			seenHeaders[header] = true
			checked.ForeignHeaders = append(checked.ForeignHeaders, header)
		}
		for _, declaration := range block.Declarations {
			switch declaration := declaration.(type) {
			case parser.ExternType:
				checkForeignTypeDeclaration(declaration, header, ctx, checked, target, &diagnostics)
			case parser.ExternFunction:
				checkForeignFunctionDeclaration(declaration, header, cSymbols, ctx, checked, target, &diagnostics)
			case parser.ExternConstant:
				checkForeignConstantDeclaration(declaration, header, cSymbols, ctx, checked, target, &diagnostics)
			case parser.ExternGlobal:
				checkForeignGlobalDeclaration(declaration, header, cSymbols, ctx, checked, target, &diagnostics)
			default:
				// The parser admits only the four declaration forms, so an
				// unknown one is a compiler-contract break.
				diagnostics = append(diagnostics, unknownAt(block.Keyword, "unrecognized foreign declaration"))
			}
		}
	}
	return diagnostics
}

// checkForeignTypeDeclaration checks one `type ...` foreign declaration: a
// transparent alias, an opaque incomplete record, or a complete record.
func checkForeignTypeDeclaration(declaration parser.ExternType, header ForeignHeader, ctx checkContext, checked *Program, target compilerTypes.TargetProfileID, diagnostics *compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	if !ctx.foreignNameAvailable(name, declaration.Name, diagnostics) {
		return
	}
	if declaration.Alias != nil {
		// A transparent alias inside the block is an ordinary Hexal alias: it
		// carries no C spelling and adds no foreign type family.
		use, diagnostic := resolveTypeUse(declaration.Alias, declaration.Name, ctx.typeEnvironment, ctx.names.generics)
		if diagnostic != nil {
			*diagnostics = append(*diagnostics, *diagnostic)
			return
		}
		if diagnostic := valueTypeDiagnostic(declaration.Alias, declaration.Name, use.Type); diagnostic != nil {
			*diagnostics = append(*diagnostics, *diagnostic)
			return
		}
		ctx.typeEnvironment.DeclareAlias(name, use.Type)
		checked.TypeDeclarations = append(checked.TypeDeclarations, TypeDeclaration{
			Name:         name,
			Type:         use.Type,
			TypeUse:      use,
			SourceLine:   declaration.Name.Line,
			SourceColumn: declaration.Name.Column,
		})
		return
	}

	cName, ok := foreignRecordCName(declaration, name, diagnostics)
	if !ok {
		return
	}
	identity := foreignRecordIdentity(cName, target)
	// Publish the record identity before resolving members so a member may
	// reach the record behind at least one pointer layer.
	record, existed := ctx.arena().ForeignRecord(identity, name, cName, true, declaration.Name.Line, declaration.Name.Column)
	ctx.typeEnvironment.DeclareAlias(name, record)

	var members []compilerTypes.ObjectMember
	if len(declaration.Members) > 0 {
		resolved, memberDiagnostics := checkForeignMembers(declaration, ctx, target)
		if len(memberDiagnostics) > 0 {
			*diagnostics = append(*diagnostics, memberDiagnostics...)
			return
		}
		members = resolved
	}
	if existed && !foreignRecordCompatible(record, members) {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "conflicting foreign declarations for C symbol "+cName))
		return
	}
	if members != nil {
		record = ctx.arena().CompleteForeignRecord(record, members)
	}
	use := compilerTypes.NewTypeUse(record)
	ctx.typeEnvironment.DeclareAlias(name, record)
	checked.ForeignRecords = append(checked.ForeignRecords, ForeignRecordDeclaration{
		Name:         name,
		CName:        cName,
		Header:       header,
		Type:         record,
		TypeUse:      use,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	})
}

// checkForeignFunctionDeclaration resolves one foreign function's signature.
func checkForeignFunctionDeclaration(declaration parser.ExternFunction, header ForeignHeader, cSymbols map[string]string, ctx checkContext, checked *Program, target compilerTypes.TargetProfileID, diagnostics *compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	if !ctx.foreignNameAvailable(name, declaration.Name, diagnostics) {
		return
	}
	cName := name
	if declaration.CName != nil {
		cName = declaration.CName.Lexeme
	}
	if !foreignIdentifier(cName) {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "invalid C spelling "+cName))
		return
	}
	if _, exists := cSymbols[cName]; exists {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "conflicting foreign declarations for C symbol "+cName))
		return
	}

	written := make([]parser.Parameter, 0, len(declaration.Parameters))
	for _, parameter := range declaration.Parameters {
		written = append(written, parser.Parameter{Name: parameter.Name, Type: parameter.Type})
	}
	// Foreign ABI ownership comes before the shared signature shape check: an
	// opaque record by value is an incomplete-foreign-type error, not the
	// generic shallow-copy diagnostic.
	for _, parameter := range declaration.Parameters {
		if use, diagnostic := resolveTypeUse(parameter.Type, parameter.Name, ctx.typeEnvironment, ctx.names.generics); diagnostic == nil {
			if foreignDiagnostic := checkForeignValueType(use.Type, parameter.Name, target); foreignDiagnostic != nil {
				*diagnostics = append(*diagnostics, *foreignDiagnostic)
				return
			}
		}
	}
	if declaration.Result != nil {
		if use, diagnostic := resolveTypeUse(declaration.Result, declaration.Name, ctx.typeEnvironment, ctx.names.generics); diagnostic == nil {
			if foreignDiagnostic := checkForeignValueType(use.Type, declaration.Name, target); foreignDiagnostic != nil {
				*diagnostics = append(*diagnostics, *foreignDiagnostic)
				return
			}
		}
	}
	signature, signatureDiagnostics := checkFunctionSignature(written, declaration.Result, declaration.Name, ctx.names.generics, ctx.typeEnvironment)
	if len(signatureDiagnostics) > 0 {
		*diagnostics = append(*diagnostics, signatureDiagnostics...)
		return
	}
	// A foreign parameter and result must each have a supported C ABI mapping
	// for the selected target, and a recorded `as` spelling must be compatible
	// with the checked Hexal type.
	for index := range signature.parameters {
		parameter := &signature.parameters[index]
		written := declaration.Parameters[index]
		parameterDiagnostics := checkForeignABIPosition(parameter.Type, written.CType, written.Name, ctx, target)
		*diagnostics = append(*diagnostics, parameterDiagnostics...)
		if len(parameterDiagnostics) > 0 {
			return
		}
		if written.CType != nil {
			parameter.CName = written.CType.Lexeme
		}
	}
	resultCName := ""
	if signature.result != nil {
		resultDiagnostics := checkForeignABIPosition(*signature.result, declaration.ResultCType, declaration.Name, ctx, target)
		*diagnostics = append(*diagnostics, resultDiagnostics...)
		if len(resultDiagnostics) > 0 {
			return
		}
		if declaration.ResultCType != nil {
			resultCName = declaration.ResultCType.Lexeme
		}
	}

	cSymbols[cName] = "function"
	ctx.names.module[name] = binding{
		typ:               signature.functionType,
		use:               compilerTypes.FunctionTypeUse(signature.functionType, parameterUses(signature.parameters), signature.resultUse),
		kind:              foreignFunctionBinding,
		foreignCName:      cName,
		foreignHeader:     header,
		foreignParameters: foreignParameterSpellings(signature.parameters),
		foreignResult:     resultCName,
	}
	checked.ForeignFunctions = append(checked.ForeignFunctions, ForeignFunctionDeclaration{
		Name:         name,
		CName:        cName,
		Header:       header,
		Parameters:   signature.parameters,
		Result:       signature.result,
		ResultUse:    signature.resultUse,
		ResultCName:  resultCName,
		Type:         signature.functionType,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	})
}

// checkForeignConstantDeclaration resolves one foreign constant's type.
func checkForeignConstantDeclaration(declaration parser.ExternConstant, header ForeignHeader, cSymbols map[string]string, ctx checkContext, checked *Program, target compilerTypes.TargetProfileID, diagnostics *compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	if !ctx.foreignNameAvailable(name, declaration.Name, diagnostics) {
		return
	}
	cName := name
	if declaration.CName != nil {
		cName = declaration.CName.Lexeme
	}
	if !foreignIdentifier(cName) {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "invalid C spelling "+cName))
		return
	}
	if _, exists := cSymbols[cName]; exists {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "conflicting foreign declarations for C symbol "+cName))
		return
	}
	use, diagnostic := resolveTypeUse(declaration.Type, declaration.Name, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		*diagnostics = append(*diagnostics, *diagnostic)
		return
	}
	if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, use.Type); diagnostic != nil {
		*diagnostics = append(*diagnostics, *diagnostic)
		return
	}
	// A foreign constant names a side-effect-free C expression: a scalar, a
	// data pointer, or a complete foreign record qualifies. A function
	// pointer, an opaque record, or an aggregate without a value
	// representation does not.
	if !foreignConstantTypeAllowed(use.Type) {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, use.Type.Name+" has no supported C ABI mapping for target "+string(target)))
		return
	}
	cSymbols[cName] = "constant"
	ctx.names.module[name] = binding{typ: use.Type, use: use, kind: foreignConstantBinding, foreignCName: cName, foreignHeader: header}
	checked.ForeignConstants = append(checked.ForeignConstants, ForeignConstantDeclaration{
		Name:         name,
		CName:        cName,
		Header:       header,
		Type:         use.Type,
		TypeUse:      use,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	})
}

// foreignConstantTypeAllowed reports whether one resolved type may be a
// foreign constant's type. A foreign constant lowers to its exact C symbol and
// is read without unsafe, so the value must be a side-effect-free C
// expression: a scalar (integer, float, rune, or bool), a data pointer, or a
// complete foreign record. A function pointer and an opaque (incomplete)
// foreign record are rejected.
func foreignConstantTypeAllowed(typ compilerTypes.Type) bool {
	if compilerTypes.IsInteger(typ) || compilerTypes.IsFloat(typ) || compilerTypes.IsRune(typ) || isBool(typ) {
		return true
	}
	if compilerTypes.IsForeignRecord(typ) {
		return !compilerTypes.ForeignRecordIncomplete(typ)
	}
	return isDataPointer(typ)
}

// isDataPointer reports whether typ is a pointer to a value, possibly wrapped
// in a nullable form. A function pointer is not a data pointer.
func isDataPointer(typ compilerTypes.Type) bool {
	if typ.Signature != nil {
		return false
	}
	if typ.Element != nil {
		return true
	}
	if typ.NullableBase != nil {
		return isDataPointer(*typ.NullableBase)
	}
	return false
}

// checkForeignGlobalDeclaration resolves one foreign global's type.
func checkForeignGlobalDeclaration(declaration parser.ExternGlobal, header ForeignHeader, cSymbols map[string]string, ctx checkContext, checked *Program, target compilerTypes.TargetProfileID, diagnostics *compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	if !ctx.foreignNameAvailable(name, declaration.Name, diagnostics) {
		return
	}
	cName := name
	if declaration.CName != nil {
		cName = declaration.CName.Lexeme
	}
	if !foreignIdentifier(cName) {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "invalid C spelling "+cName))
		return
	}
	if _, exists := cSymbols[cName]; exists {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "conflicting foreign declarations for C symbol "+cName))
		return
	}
	use, diagnostic := resolveTypeUse(declaration.Type, declaration.Name, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		*diagnostics = append(*diagnostics, *diagnostic)
		return
	}
	if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, use.Type); diagnostic != nil {
		*diagnostics = append(*diagnostics, *diagnostic)
		return
	}
	if diagnostic := checkForeignValueType(use.Type, declaration.Name, target); diagnostic != nil {
		*diagnostics = append(*diagnostics, *diagnostic)
		return
	}
	cSymbols[cName] = "global"
	ctx.names.module[name] = binding{typ: use.Type, use: use, kind: foreignGlobalBinding, mutable: declaration.Mutable, foreignCName: cName, foreignHeader: header}
	checked.ForeignGlobals = append(checked.ForeignGlobals, ForeignGlobalDeclaration{
		Name:         name,
		CName:        cName,
		Header:       header,
		Type:         use.Type,
		TypeUse:      use,
		Mutable:      declaration.Mutable,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	})
}

// foreignNameAvailable reports whether name can be bound at module scope and
// records the ordinary duplicate-name diagnostic otherwise.
func (ctx checkContext) foreignNameAvailable(name string, token lexer.Token, diagnostics *compilerTypes.Diagnostics) bool {
	if _, exists := ctx.names.module[name]; exists {
		*diagnostics = append(*diagnostics, typeErrorAt(token, name+" is already declared"))
		return false
	}
	if compilerTypes.IsProtectedTypeName(name) || ctx.typeEnvironment.Contains(name) {
		*diagnostics = append(*diagnostics, typeErrorAt(token, "value "+name+" is already declared as a type"))
		return false
	}
	return true
}

// checkForeignMembers resolves one complete foreign record's members in
// declaration order. A member's Hexal name defaults to its C spelling; `as`
// supplies the exact C field name when they differ.
func checkForeignMembers(declaration parser.ExternType, ctx checkContext, target compilerTypes.TargetProfileID) ([]compilerTypes.ObjectMember, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	members := make([]compilerTypes.ObjectMember, 0, len(declaration.Members))
	seen := make(map[string]bool, len(declaration.Members))
	for _, member := range declaration.Members {
		name := member.Name.Lexeme
		if seen[name] {
			diagnostics = append(diagnostics, typeErrorAt(member.Name, "member "+name+" is declared more than once"))
			continue
		}
		seen[name] = true
		use, diagnostic := resolveTypeUse(member.Type, member.Name, ctx.typeEnvironment, ctx.names.generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if diagnostic := valueTypeDiagnostic(member.Type, member.Name, use.Type); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if diagnostic := checkForeignValueType(use.Type, member.Name, target); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		cName := name
		if member.CName != nil {
			cName = member.CName.Lexeme
			if !foreignIdentifier(cName) {
				diagnostics = append(diagnostics, typeErrorAt(member.Name, "invalid C spelling "+cName))
				continue
			}
		}
		members = append(members, compilerTypes.ObjectMember{
			Name:         name,
			CName:        cName,
			Type:         use.Type,
			Use:          use,
			Mutable:      member.Mutable,
			SourceLine:   member.Name.Line,
			SourceColumn: member.Name.Column,
		})
	}
	return members, diagnostics
}

// foreignRecordCName resolves the exact C spelling of a foreign record: the
// declared `as` name, or the Hexal name as a typedef spelling.
func foreignRecordCName(declaration parser.ExternType, name string, diagnostics *compilerTypes.Diagnostics) (string, bool) {
	if declaration.CName == nil {
		return name, true
	}
	cName := declaration.CName.Lexeme
	if !foreignRecordSpelling(cName) {
		*diagnostics = append(*diagnostics, typeErrorAt(declaration.Name, "invalid C spelling "+cName))
		return "", false
	}
	return cName, true
}

// foreignRecordCompatible reports whether a repeat declaration of an already
// interned foreign record agrees with the first. Completing an opaque
// declaration is compatible; a field or qualification mismatch is not.
func foreignRecordCompatible(record compilerTypes.Type, members []compilerTypes.ObjectMember) bool {
	if members == nil {
		// An opaque repeat never contradicts a complete definition: the
		// complete definition remains authoritative.
		return true
	}
	existing := compilerTypes.ForeignRecordMembers(record)
	if len(existing) == 0 {
		// The record was opaque; this completes it.
		return true
	}
	if len(existing) != len(members) {
		return false
	}
	for index := range members {
		if existing[index].Name != members[index].Name ||
			existing[index].CName != members[index].CName ||
			existing[index].Mutable != members[index].Mutable ||
			!compilerTypes.Equal(existing[index].Type, members[index].Type) {
			return false
		}
	}
	return true
}

// checkForeignABIPosition validates one foreign parameter or result position:
// the Hexal type must have a supported ABI mapping, and a recorded `as`
// spelling must be compatible with it.
func checkForeignABIPosition(typ compilerTypes.Type, spelling *lexer.Token, token lexer.Token, ctx checkContext, target compilerTypes.TargetProfileID) compilerTypes.Diagnostics {
	if diagnostic := checkForeignValueType(typ, token, target); diagnostic != nil {
		return compilerTypes.Diagnostics{*diagnostic}
	}
	if spelling == nil {
		return nil
	}
	if !foreignCSpellingCompatible(typ, spelling.Lexeme, target) {
		diagnostic := typeErrorAt(token,
			"C spelling "+spelling.Lexeme+" cannot be proven in a handwritten binding; use an automatic C import or expose a C wrapper")
		return compilerTypes.Diagnostics{diagnostic}
	}
	return nil
}

// foreignIncompletePlacementDiagnostic reports an opaque foreign record used
// where a complete value is required. The position names the forbidden
// operation so the diagnostic matches the exact contract form.
func foreignIncompletePlacementDiagnostic(record compilerTypes.Type, token lexer.Token, position string) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "foreign type "+record.Name+" is incomplete in "+position)
	return &diagnostic
}

// checkForeignValueType rejects a foreign value position whose type cannot
// cross the C ABI: an opaque record by value or a non-ABI composite.
func checkForeignValueType(typ compilerTypes.Type, token lexer.Token, target compilerTypes.TargetProfileID) *compilerTypes.Diagnostic {
	if typ.NullableBase != nil {
		return checkForeignValueType(*typ.NullableBase, token, target)
	}
	if typ.Element != nil {
		// A pointer crosses the ABI whatever it points at; an opaque pointee
		// is the intended way to hold an incomplete C type.
		return nil
	}
	if typ.Signature != nil {
		return diagnosticAt(typeErrorAt(token, typ.Name+" has no supported C ABI mapping for target "+string(target)))
	}
	if typ.Object != nil {
		if compilerTypes.ForeignRecordIncomplete(typ) {
			return diagnosticAt(typeErrorAt(token, "foreign type "+typ.Name+" is incomplete in a value position"))
		}
		return nil
	}
	if compilerTypes.IsInteger(typ) || compilerTypes.IsFloat(typ) || isBool(typ) || compilerTypes.IsRune(typ) || compilerTypes.IsSize(typ) {
		return nil
	}
	return diagnosticAt(typeErrorAt(token, typ.Name+" has no supported C ABI mapping for target "+string(target)))
}

// foreignCSpellingCompatible reports whether one restricted C spelling matches
// a checked Hexal type's representation.
func foreignCSpellingCompatible(typ compilerTypes.Type, spelling string, target compilerTypes.TargetProfileID) bool {
	// Nullable is a Hexal-only fact: the C spelling describes the pointer.
	if typ.NullableBase != nil {
		typ = *typ.NullableBase
	}
	depth, constLayers, base := splitCSpelling(spelling)
	if base == "" {
		return false
	}
	if typ.Element != nil {
		if depth == 0 {
			return false
		}
		if constLayers[depth-1] {
			// A const-qualified pointee is read-only: it matches Ptr<T>.
			if typ.PointeeWritable {
				return false
			}
		} else if !typ.PointeeWritable {
			return false
		}
		if depth > 1 {
			// A nested pointer layer is checked recursively against the
			// pointee, which must itself be a pointer.
			if typ.Element.Element == nil {
				return false
			}
			inner := *typ.Element
			innerSpelling := strings.Repeat("*", depth-1) + " " + base
			return foreignCSpellingCompatible(inner, innerSpelling, target)
		}
		return foreignPointeeCompatible(*typ.Element, base, target)
	}
	if depth != 0 {
		return false
	}
	return foreignScalarCompatible(typ, base, target)
}

// foreignPointeeCompatible matches a C pointee spelling against a Hexal pointee
// type: a scalar, `void`, or a declared foreign record.
func foreignPointeeCompatible(typ compilerTypes.Type, base string, target compilerTypes.TargetProfileID) bool {
	if typ.NullableBase != nil {
		return foreignPointeeCompatible(*typ.NullableBase, base, target)
	}
	if compilerTypes.IsUnknown(typ) && strings.TrimSpace(base) == "void" {
		return true
	}
	if typ.Element != nil {
		return false
	}
	if typ.Object != nil {
		return strings.TrimSpace(base) == strings.TrimSpace(typ.CName)
	}
	return foreignScalarCompatible(typ, base, target)
}

// foreignScalarCompatible reports whether a C scalar base spelling maps to the
// given Hexal scalar on the selected target. A char base matches Byte and any
// 8-bit integer scalar because a C char pointer is the Byte-buffer bridge.
func foreignScalarCompatible(typ compilerTypes.Type, base string, target compilerTypes.TargetProfileID) bool {
	mapped, ok := foreignScalarForSpelling(base, target)
	if !ok {
		return false
	}
	if compilerTypes.Equal(mapped, typ) {
		return true
	}
	return isCharSpelling(base) && compilerTypes.IsInteger(typ) && typ.Bits == 8
}

// splitCSpelling splits a restricted C type spelling into its pointer layer
// count, one const flag per layer (outermost last), and the base spelling with
// `const` qualifiers removed. An empty base means the spelling is not one the
// restricted grammar accepts.
func splitCSpelling(spelling string) (int, []bool, string) {
	trimmed := strings.TrimSpace(spelling)
	depth := 0
	for strings.HasSuffix(trimmed, "*") {
		depth++
		trimmed = strings.TrimSuffix(trimmed, "*")
		trimmed = strings.TrimSpace(trimmed)
	}
	constLayers := make([]bool, depth)
	words := strings.Fields(trimmed)
	kept := make([]string, 0, len(words))
	for _, word := range words {
		switch word {
		case "const":
			continue
		case "volatile", "restrict", "_Atomic":
			return 0, nil, ""
		default:
			kept = append(kept, word)
		}
	}
	// A single leading `const` qualifies the outermost pointee; C's exact
	// const placement is not modelled.
	if len(words) > 0 && words[0] == "const" && depth > 0 {
		constLayers[depth-1] = true
	}
	return depth, constLayers, strings.Join(kept, " ")
}

// foreignScalarForSpelling maps a normalized restricted C scalar spelling to
// the Hexal scalar it represents on the selected target.
func foreignScalarForSpelling(spelling string, target compilerTypes.TargetProfileID) (compilerTypes.Type, bool) {
	switch strings.TrimSpace(spelling) {
	case "bool", "_Bool":
		return compilerTypes.Bool, true
	case "char", "signed char":
		return compilerTypes.Int8, true
	case "unsigned char":
		return compilerTypes.UInt8, true
	case "short", "short int", "signed short", "signed short int":
		return compilerTypes.Int16, true
	case "unsigned short", "unsigned short int":
		return compilerTypes.UInt16, true
	case "int", "signed", "signed int":
		return compilerTypes.Int32, true
	case "unsigned", "unsigned int":
		return compilerTypes.UInt32, true
	case "long", "long int", "signed long", "signed long int":
		if target == compilerTypes.TargetX86_64WindowsGNU {
			return compilerTypes.Int32, true
		}
		return compilerTypes.Int64, true
	case "unsigned long", "unsigned long int":
		if target == compilerTypes.TargetX86_64WindowsGNU {
			return compilerTypes.UInt32, true
		}
		return compilerTypes.UInt64, true
	case "long long", "long long int", "signed long long", "signed long long int":
		return compilerTypes.Int64, true
	case "unsigned long long", "unsigned long long int":
		return compilerTypes.UInt64, true
	case "float":
		return compilerTypes.Float32, true
	case "double":
		return compilerTypes.Float64, true
	case "size_t":
		return compilerTypes.SizeType, true
	case "int8_t":
		return compilerTypes.Int8, true
	case "int16_t":
		return compilerTypes.Int16, true
	case "int32_t":
		return compilerTypes.Int32, true
	case "int64_t":
		return compilerTypes.Int64, true
	case "uint8_t":
		return compilerTypes.UInt8, true
	case "uint16_t":
		return compilerTypes.UInt16, true
	case "uint32_t":
		return compilerTypes.UInt32, true
	case "uint64_t":
		return compilerTypes.UInt64, true
	default:
		return compilerTypes.Type{}, false
	}
}

// isCharSpelling reports whether a base spelling is one of C's plain or
// qualified char types, the Byte-buffer bridge.
func isCharSpelling(base string) bool {
	switch strings.TrimSpace(base) {
	case "char", "signed char", "unsigned char":
		return true
	default:
		return false
	}
}

// foreignIdentifier reports whether text is one ordinary C identifier.
func foreignIdentifier(text string) bool {
	if text == "" {
		return false
	}
	for index, character := range text {
		letter := (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_'
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
	return true
}

// foreignRecordSpelling reports whether text is one C typedef name or one tag
// spelling (`struct X` or `union X`).
func foreignRecordSpelling(text string) bool {
	trimmed := strings.TrimSpace(text)
	if foreignIdentifier(trimmed) {
		return true
	}
	for _, prefix := range []string{"struct ", "union "} {
		if strings.HasPrefix(trimmed, prefix) && foreignIdentifier(strings.TrimSpace(trimmed[len(prefix):])) {
			return true
		}
	}
	return false
}

// foreignRecordIdentity keys a foreign record by target and canonical C
// identity: the tag namespace plus exact tag spelling, or the typedef spelling.
func foreignRecordIdentity(cName string, target compilerTypes.TargetProfileID) string {
	return string(target) + "\x00" + strings.TrimSpace(cName)
}

// configurationErrorAt reports a compilation configuration failure owned by
// the checker's foreign surface.
func configurationErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.ConfigurationError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func isBool(typ compilerTypes.Type) bool {
	return compilerTypes.Equal(typ, compilerTypes.Bool)
}

func (ctx checkContext) arena() *compilerTypes.Arena {
	return ctx.typeEnvironment.Arena()
}

func parameterUses(parameters []FunctionParameter) []compilerTypes.TypeUse {
	uses := make([]compilerTypes.TypeUse, 0, len(parameters))
	for _, parameter := range parameters {
		uses = append(uses, parameter.TypeUse)
	}
	return uses
}

// foreignParameterSpellings collects the exact C spelling each foreign
// parameter records, in position order. An empty entry means the parameter
// passes without a boundary cast.
func foreignParameterSpellings(parameters []FunctionParameter) []string {
	spellings := make([]string, len(parameters))
	for index, parameter := range parameters {
		spellings[index] = parameter.CName
	}
	return spellings
}
