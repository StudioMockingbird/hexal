package checker

import (
	"go/constant"
	"strconv"
	"unicode/utf8"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// Strings are reference-like handles with C-style shallow copies and manual
// cleanup; the flow state tracks possible storage origins per String place
// so a statically proven literal free is rejected and anything else falls
// through to the runtime discriminator.

// stringOriginSet is a bitmask of possible String storage origins for one
// place. Zero reads as opaque: parameters, call results, and untracked
// places carry no record and are enforced by the runtime check instead.
type stringOriginSet uint8

const (
	stringOriginStatic stringOriginSet = 1
	stringOriginOwned  stringOriginSet = 2
	stringOriginOpaque stringOriginSet = 4
)

// stringPlaceKey names one member or element origin record: root is the
// owning binding, member the object or payload field name, or the constant
// array index when indexed.
type stringPlaceKey struct {
	root    BindingID
	member  string
	index   uint64
	indexed bool
}

// stringOrigin reads one binding's recorded origins, or opaque when the
// binding carries no record.
func (state *flowState) stringOrigin(id BindingID) stringOriginSet {
	if state == nil {
		return stringOriginOpaque
	}
	if set, ok := state.stringOrigins[id]; ok {
		return set
	}
	return stringOriginOpaque
}

// setStringOrigin replaces one binding's recorded origins.
func (state *flowState) setStringOrigin(id BindingID, set stringOriginSet) {
	if state == nil {
		return
	}
	if state.stringOrigins == nil {
		state.stringOrigins = make(map[BindingID]stringOriginSet)
	}
	state.stringOrigins[id] = set
}

// stringPlaceOrigin reads one member or element record, or opaque when the
// place carries no record.
func (state *flowState) stringPlaceOrigin(key stringPlaceKey) stringOriginSet {
	if state == nil {
		return stringOriginOpaque
	}
	if set, ok := state.stringPlaces[key]; ok {
		return set
	}
	return stringOriginOpaque
}

// setStringPlaceOrigin replaces one member or element record.
func (state *flowState) setStringPlaceOrigin(key stringPlaceKey, set stringOriginSet) {
	if state == nil {
		return
	}
	if state.stringPlaces == nil {
		state.stringPlaces = make(map[stringPlaceKey]stringOriginSet)
	}
	state.stringPlaces[key] = set
}

// unionStringElements unions the recorded origins of every element of the
// array binding, for dynamic-index reads. No record reads opaque.
func (state *flowState) unionStringElements(root BindingID) stringOriginSet {
	if state == nil {
		return stringOriginOpaque
	}
	merged := stringOriginSet(0)
	found := false
	for key, set := range state.stringPlaces {
		if key.root == root && key.indexed {
			merged |= set
			found = true
		}
	}
	if !found {
		return stringOriginOpaque
	}
	return merged
}

// joinStringElements unions one origin set into every recorded element of
// the array binding, for dynamic-index assignment.
func (state *flowState) joinStringElements(root BindingID, set stringOriginSet) {
	if state == nil {
		return
	}
	for key, recorded := range state.stringPlaces {
		if key.root == root && key.indexed {
			state.stringPlaces[key] = recorded | set
		}
	}
}

// dropStringPlaces removes every member and element record rooted at id, for
// escape: an unseen write may have replaced any of them, so later reads
// fall through to the runtime check.
func (state *flowState) dropStringPlaces(root BindingID) {
	if state == nil {
		return
	}
	for key := range state.stringPlaces {
		if key.root == root {
			delete(state.stringPlaces, key)
		}
	}
}

// stringPlaceRoot resolves a member or element receiver to its owning
// binding when the receiver is a single-level variable use. Deeper receiver
// chains have no recorded place and read opaque.
func stringPlaceRoot(node *Expression, ctx checkContext) (BindingID, bool) {
	if node == nil || node.Kind != VariableExpression || node.Binding == 0 {
		return 0, false
	}
	return node.Binding, true
}

// stringIndexConstant reads a constant array index operand, reporting
// whether the index is statically known.
func stringIndexConstant(argument Operand) (uint64, bool) {
	if argument.Constant == nil || argument.Constant.Kind() != constant.Int {
		return 0, false
	}
	value, exact := constant.Uint64Val(argument.Constant)
	if !exact {
		return 0, false
	}
	return value, true
}

// adtPayloadFieldName resolves an ADT payload access to its declared field
// name from the receiver's stored variant layout.
func adtPayloadFieldName(node Expression) (string, bool) {
	if node.OperandType.Adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(node.OperandType.Adt.Variants) {
		return "", false
	}
	payload := node.OperandType.Adt.Variants[node.VariantIndex].Payload
	if node.MemberIndex < 0 || node.MemberIndex >= len(payload) {
		return "", false
	}
	return payload[node.MemberIndex].Name, true
}

// stringOriginOf classifies one checked value's possible String storage
// origins: literals are static, direct allocating constructors owned, and
// every other source consults the recorded place or falls back to opaque.
// Variable, member, index, and payload reads observe the current flow facts;
// match arms and try operands union transparently like control-flow joins.
func stringOriginOf(node Expression, ctx checkContext) stringOriginSet {
	switch node.Kind {
	case StringLiteralExpression:
		return stringOriginStatic
	case StringFromBytesExpression, StringInterpolateExpression:
		return stringOriginOwned
	case StringMethodCallExpression:
		switch node.Name {
		case "concat", "copy":
			return stringOriginOwned
		}
		return stringOriginOpaque
	case VariableExpression:
		if ctx.names.flow != nil && node.Binding != 0 {
			return ctx.names.flow.stringOrigin(node.Binding)
		}
		return stringOriginOpaque
	case MemberExpression:
		if root, ok := stringPlaceRoot(node.Operand, ctx); ok && node.Member != nil {
			return ctx.names.flow.stringPlaceOrigin(stringPlaceKey{root: root, member: node.Member.Name})
		}
		return stringOriginOpaque
	case IndexExpression:
		root, ok := stringPlaceRoot(node.Operand, ctx)
		if !ok || len(node.Arguments) == 0 {
			return stringOriginOpaque
		}
		if index, known := stringIndexConstant(node.Arguments[0]); known {
			return ctx.names.flow.stringPlaceOrigin(stringPlaceKey{root: root, index: index, indexed: true})
		}
		return ctx.names.flow.unionStringElements(root)
	case AdtPayloadExpression:
		root, ok := stringPlaceRoot(node.Operand, ctx)
		if !ok {
			return stringOriginOpaque
		}
		if field, ok := adtPayloadFieldName(node); ok {
			return ctx.names.flow.stringPlaceOrigin(stringPlaceKey{root: root, member: field})
		}
		return stringOriginOpaque
	case UnionInjectionExpression, UnionWidenExpression, TryExpression:
		if node.Operand != nil {
			return stringOriginOf(*node.Operand, ctx)
		}
		return stringOriginOpaque
	case MatchExpression:
		merged := stringOriginSet(0)
		for _, argument := range node.Arguments {
			merged |= stringOriginOf(argument.Node, ctx)
		}
		if len(node.Arguments) == 0 {
			return stringOriginOpaque
		}
		return merged
	}
	return stringOriginOpaque
}

// recordStringBinding records a new binding's whole-value origin plus
// per-member origins for inline construction, so later reads observe the
// same static possibilities the initializer spelled.
func recordStringBinding(flow *flowState, id BindingID, node Expression, ctx checkContext) {
	if flow == nil || id == 0 {
		return
	}
	flow.setStringOrigin(id, stringOriginOf(node, ctx))
	switch node.Kind {
	case ObjectExpression:
		if node.Object == nil {
			return
		}
		for _, initialized := range node.Object.Initializers {
			if initialized.Member == nil {
				continue
			}
			flow.setStringPlaceOrigin(stringPlaceKey{root: id, member: initialized.Member.Name}, stringOriginOf(initialized.Source.Node, ctx))
		}
	case AdtConstructExpression:
		if node.OperandType.Adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(node.OperandType.Adt.Variants) {
			return
		}
		payload := node.OperandType.Adt.Variants[node.VariantIndex].Payload
		for index, argument := range node.Arguments {
			if index >= len(payload) {
				break
			}
			flow.setStringPlaceOrigin(stringPlaceKey{root: id, member: payload[index].Name}, stringOriginOf(argument.Node, ctx))
		}
	case ArrayLiteralExpression:
		for index, argument := range node.Arguments {
			flow.setStringPlaceOrigin(stringPlaceKey{root: id, index: uint64(index), indexed: true}, stringOriginOf(argument.Node, ctx))
		}
	}
}

// recordStringAssignment replaces the origin record a successful assignment
// wrote: a binding's whole-value set, a known member or constant element's
// set, or a dynamic element write joined into every recorded element.
func recordStringAssignment(flow *flowState, target Operand, value Operand, targetBinding BindingID, ctx checkContext) {
	if flow == nil {
		return
	}
	set := stringOriginOf(value.Node, ctx)
	if targetBinding != 0 {
		flow.setStringOrigin(targetBinding, set)
		return
	}
	switch target.Node.Kind {
	case MemberExpression:
		root, ok := stringPlaceRoot(target.Node.Operand, ctx)
		if !ok || target.Node.Member == nil {
			return
		}
		flow.setStringPlaceOrigin(stringPlaceKey{root: root, member: target.Node.Member.Name}, set)
	case AdtPayloadExpression:
		root, ok := stringPlaceRoot(target.Node.Operand, ctx)
		if !ok {
			return
		}
		if field, ok := adtPayloadFieldName(target.Node); ok {
			flow.setStringPlaceOrigin(stringPlaceKey{root: root, member: field}, set)
		}
	case IndexExpression:
		root, ok := stringPlaceRoot(target.Node.Operand, ctx)
		if !ok || len(target.Node.Arguments) == 0 {
			return
		}
		if index, known := stringIndexConstant(target.Node.Arguments[0]); known {
			flow.setStringPlaceOrigin(stringPlaceKey{root: root, index: index, indexed: true}, set)
			return
		}
		flow.joinStringElements(root, set)
	}
}

// decodeStringLiteral decodes a double-quoted literal's raw lexeme (including
// the surrounding quotes) into its payload bytes using the shared literal
// decoder, which validates every escape and UTF-8 validity.
func decodeStringLiteral(token lexer.Token) ([]byte, *compilerTypes.Diagnostic) {
	raw := token.Lexeme
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, diagnosticAt(typeErrorAt(token, "malformed string literal"))
	}
	payload, message := lexer.DecodeLiteralBody(raw[1:len(raw)-1], lexer.StringEscapes)
	if message != "" {
		return nil, diagnosticAt(typeErrorAt(token, message))
	}
	return payload, nil
}

// decodeRawStringLiteral decodes a raw string literal's raw lexeme
// (including its 'r', hash delimiters, and surrounding quotes) into its
// payload bytes: copied byte-for-byte from source with no escape or
// interpolation processing, validated only for UTF-8.
func decodeRawStringLiteral(token lexer.Token) ([]byte, *compilerTypes.Diagnostic) {
	raw := token.Lexeme
	if len(raw) < 2 || raw[0] != 'r' {
		return nil, diagnosticAt(typeErrorAt(token, "malformed raw string literal"))
	}
	hashCount := 0
	for 1+hashCount < len(raw) && raw[1+hashCount] == '#' {
		hashCount++
	}
	openEnd := 1 + hashCount + 1 // past 'r', the hashes, and the opening '"'
	closeStart := len(raw) - hashCount - 1
	if openEnd > len(raw) || closeStart < openEnd || closeStart >= len(raw) || raw[openEnd-1] != '"' || raw[closeStart] != '"' {
		return nil, diagnosticAt(typeErrorAt(token, "malformed raw string literal"))
	}
	payload := []byte(raw[openEnd:closeStart])
	if !utf8.Valid(payload) {
		return nil, diagnosticAt(typeErrorAt(token, "string literal contains invalid UTF-8"))
	}
	return payload, nil
}

// decodeByteLiteral decodes a b'...' literal into its single byte.
func decodeByteLiteral(token lexer.Token) (byte, *compilerTypes.Diagnostic) {
	raw := token.Lexeme
	if len(raw) < 4 || raw[0] != 'b' || raw[1] != '\'' || raw[len(raw)-1] != '\'' {
		return 0, diagnosticAt(typeErrorAt(token, "malformed Byte literal"))
	}
	payload, message := lexer.DecodeLiteralBody(raw[2:len(raw)-1], lexer.ByteEscapes)
	if message != "" || len(payload) != 1 {
		return 0, diagnosticAt(typeErrorAt(token, "Byte literal must contain exactly one byte"))
	}
	return payload[0], nil
}

// checkByteLiteral resolves a b'...' literal into a UInt8 constant.
func checkByteLiteral(expression parser.ByteLiteral) checkedExpression {
	value, diagnostic := decodeByteLiteral(expression.Token)
	if diagnostic != nil {
		return checkedExpression{token: expression.Token, diagnostic: diagnostic}
	}
	spelling := strconv.FormatUint(uint64(value), 10)
	source := constantOperand(compilerTypes.UInt8, constant.MakeUint64(uint64(value)), spelling)
	source.Radix = DecimalRadix
	source.Node = constantNode(source)
	return checkedExpression{source: source, typ: compilerTypes.UInt8, token: expression.Token, known: &source}
}
