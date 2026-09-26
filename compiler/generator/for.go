package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// renderForStatement lowers the for-in form. The source is stabilized exactly
// once: Array places iterate in place through their address, temporary Arrays
// and inline text are materialized into one inline copy, and String, List, and
// Dict sources copy their pointer-sized handle.
//
// Index semantics: Array, Slice, List, and text bind the Size loop counter
// directly (a body `continue` lands on the loop increment). Dict loops
// pre-increment their produced-entry ordinal before the body, so a body
// `continue` never skips the increment.
func renderForStatement(body *strings.Builder, statement checker.ForStatement, state *expressionValidation, result *compilerTypes.Type, inFunction bool, indent string) error {
	sourceType := statement.Source.Type
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}

	state.loopCounter++
	loop := fmt.Sprintf("hex_for_%d", state.loopCounter)

	// Allocate every binder name once; each iteration redeclares it as a
	// fresh immutable const.
	binderNames := make([]string, len(statement.Binders))
	for index, binder := range statement.Binders {
		name, err := state.allocateBinding(binder.Binding, binder.Name, binder.Type, false)
		if err != nil {
			return err
		}
		binderNames[index] = name
	}

	var bodyText strings.Builder
	state.pushScope()
	previousLoopDepth := state.loopDepth
	state.loopDepth++
	state.loopDepths = append(state.loopDepths, len(state.deferStack))
	err := writeStatementsAt(&bodyText, statement.Body, state, statementFrame{result: result, inFunction: inFunction, defers: statement.BodyDefers}, indent+"    ")
	state.loopDepths = state.loopDepths[:len(state.loopDepths)-1]
	state.loopDepth = previousLoopDepth
	state.popScope()
	if err != nil {
		return err
	}

	loopRender := forLoopRender{loop: loop, binderNames: binderNames, bodyText: &bodyText}
	switch {
	case sourceType.Array != nil:
		return renderForSequence(body, statement, loopRender, state, indent)
	case sourceType.Slice != nil:
		return renderForSequence(body, statement, loopRender, state, indent)
	case sourceType.List != nil:
		return renderForSequence(body, statement, loopRender, state, indent)
	case compilerTypes.IsText(sourceType):
		switch {
		case compilerTypes.IsRune(statement.Binders[len(statement.Binders)-1].Type):
			return renderForRuneText(body, statement, loopRender, state, indent)
		case compilerTypes.IsGrapheme(statement.Binders[len(statement.Binders)-1].Type):
			return renderForGraphemeText(body, statement, loopRender, state, indent)
		}
		return renderForText(body, statement, loopRender, state, indent)
	case sourceType.Dict != nil:
		return renderForDict(body, statement, loopRender, state, indent)
	default:
		return unknownExpressionDiagnostic()
	}
}

// forLoopRender is one for-in statement's loop scaffolding: the C loop
// variable name, the allocated binder names in written order, and the
// accumulated body text the renderer wraps in its iteration form.
type forLoopRender struct {
	loop        string
	binderNames []string
	bodyText    *strings.Builder
}

// forStmtLineModel carries one for-loop statement line: the decided indent,
// declaration type, name, and value; each template reads the fields it spells.
type forStmtLineModel struct {
	Indent string
	Type   string
	Name   string
	Value  string
}

// forOpenModel carries one loop opener's decided pieces: Var is the counter,
// Limit the bound expression, Width the step, Dict the scanned map; each
// opener reads the fields it spells.
type forOpenModel struct {
	Indent string
	Var    string
	Limit  string
	Width  string
	Dict   string
}

// bucketBindModel carries one dict bucket read: the decided target
// declaration, the map and bucket counter, and the field name (key or value).
type bucketBindModel struct {
	Indent string
	Target string
	Dict   string
	Var    string
	Field  string
}

// textIndexModel carries one byte-indexed assignment's decided pieces.
type textIndexModel struct {
	Indent string
	Target string
	Data   string
	Index  string
}

// utf8DecodeModel carries one UTF-8 step assignment's decided pieces; Width is
// the in/out decoded-width variable.
type utf8DecodeModel struct {
	Indent string
	Target string
	Data   string
	Length string
	Offset string
	Width  string
}

// rawTextModel carries pre-rendered statement text embedded verbatim.
type rawTextModel struct {
	Text string
}

// renderForSequence lowers Array, Slice, and List iteration to a plain index
// loop over the captured source.
func renderForSequence(body *strings.Builder, statement checker.ForStatement, render forLoopRender, state *expressionValidation, indent string) error {
	loop, binderNames, bodyText := render.loop, render.binderNames, render.bodyText
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type

	var elementAccess, length string
	switch {
	case sourceType.Array != nil:
		// An addressable Array place iterates in place through one generated
		// address; a temporary Array is materialized into one inline copy.
		// The traversal boundary is the compile-time Array length.
		if statement.Source.Addressable {
			if err := renderInto(body, "module.c", "const_addr_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
				return err
			}
		} else {
			if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
				return err
			}
		}
		length = fmt.Sprintf("(size_t)(%d)", sourceType.Array.Length)
		// The counter runs over [0, N) for the same literal N the
		// accessor would test, the binder is fresh and immutable, and an
		// Array cannot be resized: the check is dead by construction and
		// the access is a direct member read.
		if statement.Source.Addressable {
			elementAccess = fmt.Sprintf("%s->data[%s_index]", loop, loop)
		} else {
			elementAccess = fmt.Sprintf("%s.data[%s_index]", loop, loop)
		}
	case sourceType.Slice != nil:
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
			return err
		}
		length = fmt.Sprintf("%s.length", loop)
		elementAccess = fmt.Sprintf("*%s(%s, (size_t)(%s_index))", sliceAtHelper(sourceType), loop, loop)
	case sourceType.List != nil:
		if err := renderInto(body, "module.c", "const_ptr_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "version_shadow", forStmtLineModel{Indent: indent, Name: loop}); err != nil {
			return err
		}
		length = fmt.Sprintf("%s->length", loop)
		elementAccess = fmt.Sprintf("*hex_list_at_%s(%s, (size_t)(%s_index))", listSuffix(sourceType), loop, loop)
	default:
		return unknownExpressionDiagnostic()
	}

	indexVariable := loop + "_index"
	if err := renderInto(body, "module.c", "for_index_open", forOpenModel{Indent: indent, Var: indexVariable, Limit: length}); err != nil {
		return err
	}
	if sourceType.List != nil {
		if err := renderInto(body, "module.c", "version_guard_open", forStmtLineModel{Indent: indent, Name: loop}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "runtime_trap", indentModel{Indent: indent}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "inner_close", indentModel{Indent: indent}); err != nil {
			return err
		}
	}
	if err := writeLineDirective(body, state.line(statement.Binders[0].Span), state.filename); err != nil {
		return err
	}
	valueBinder := statement.Binders[len(statement.Binders)-1]
	if len(statement.Binders) == 2 {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent + "    ", Type: "size_t", Name: binderNames[0], Value: indexVariable}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "for_assign", forStmtLineModel{Indent: indent, Name: declaration(valueBinder.Type, binderNames[len(binderNames)-1], false), Value: elementAccess}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: bodyText.String()}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

// renderForText lowers String and String<N> iteration to a plain byte loop:
// text is a sequence of bytes and the loop binds each one. A heap String copies
// its handle. An inline value is copied once into the loop's own storage first,
// the rule a temporary Array follows, so reassigning a mutable binding inside
// the body cannot change what the loop reads or leave its captured length
// stale. A body `continue` lands on the loop increment.
func renderForText(body *strings.Builder, statement checker.ForStatement, render forLoopRender, state *expressionValidation, indent string) error {
	loop, binderNames, bodyText := render.loop, render.binderNames, render.bodyText
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type
	var byteLength, data string
	if compilerTypes.IsInlineString(sourceType) {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
			return err
		}
		byteLength = fmt.Sprintf("%s.byte_length", loop)
		data = fmt.Sprintf("%s.data", loop)
	} else {
		if err := renderInto(body, "module.c", "const_ptr_decl", forStmtLineModel{Indent: indent, Type: "hex_string", Name: loop, Value: source}); err != nil {
			return err
		}
		byteLength = fmt.Sprintf("%s->byte_length", loop)
		data = fmt.Sprintf("%s->data", loop)
	}

	indexVariable := loop + "_index"
	hasIndex := len(statement.Binders) == 2
	if err := renderInto(body, "module.c", "for_index_open", forOpenModel{Indent: indent, Var: indexVariable, Limit: byteLength}); err != nil {
		return err
	}
	if err := writeLineDirective(body, state.line(statement.Binders[0].Span), state.filename); err != nil {
		return err
	}
	valueBinder := statement.Binders[len(statement.Binders)-1]
	if hasIndex {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent + "    ", Type: "size_t", Name: binderNames[0], Value: indexVariable}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "text_index_assign", textIndexModel{
		Indent: indent,
		Target: declaration(valueBinder.Type, binderNames[len(binderNames)-1], false),
		Data:   data,
		Index:  indexVariable,
	}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: bodyText.String()}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

// renderForRuneText lowers Rune iteration of String and String<N>: text is
// validated UTF-8, so the loop decodes one scalar per step with the utf8proc
// adapter the validator uses. The loop variable is the byte offset, and the
// increment advances by the width decoded at the top of the body, so a body
// `continue` still advances exactly one scalar. A heap String copies its
// handle; an inline value is copied once into the loop's own storage, the rule
// a temporary Array follows.
func renderForRuneText(body *strings.Builder, statement checker.ForStatement, render forLoopRender, state *expressionValidation, indent string) error {
	loop, binderNames, bodyText := render.loop, render.binderNames, render.bodyText
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type
	var byteLength, data string
	if compilerTypes.IsInlineString(sourceType) {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
			return err
		}
		byteLength = fmt.Sprintf("%s.byte_length", loop)
		data = fmt.Sprintf("%s.data", loop)
	} else {
		if err := renderInto(body, "module.c", "const_ptr_decl", forStmtLineModel{Indent: indent, Type: "hex_string", Name: loop, Value: source}); err != nil {
			return err
		}
		byteLength = fmt.Sprintf("%s->byte_length", loop)
		data = fmt.Sprintf("%s->data", loop)
	}

	offsetVariable := loop + "_offset"
	widthVariable := loop + "_width"
	hasIndex := len(statement.Binders) == 2
	if err := renderInto(body, "module.c", "ordinal_decl", forStmtLineModel{Indent: indent, Name: widthVariable, Value: "0"}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "for_width_open", forOpenModel{Indent: indent, Var: offsetVariable, Limit: byteLength, Width: widthVariable}); err != nil {
		return err
	}
	if err := writeLineDirective(body, state.line(statement.Binders[0].Span), state.filename); err != nil {
		return err
	}
	valueBinder := statement.Binders[len(statement.Binders)-1]
	if hasIndex {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent + "    ", Type: "size_t", Name: binderNames[0], Value: offsetVariable}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "utf8_decode_assign", utf8DecodeModel{
		Indent: indent,
		Target: declaration(valueBinder.Type, binderNames[len(binderNames)-1], false),
		Data:   data,
		Length: byteLength,
		Offset: offsetVariable,
		Width:  widthVariable,
	}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: bodyText.String()}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

// renderForGraphemeText lowers Grapheme iteration of String and String<N> to
// the same stateful segmenter the GraphemeCursor uses: the loop advances by
// one cluster per step, so the break state sees every adjacent scalar pair in
// order. The loop variable is the byte offset for the index binder. A heap
// String copies its handle; an inline value is copied once into the loop's own
// storage.
func renderForGraphemeText(body *strings.Builder, statement checker.ForStatement, render forLoopRender, state *expressionValidation, indent string) error {
	loop, binderNames, bodyText := render.loop, render.binderNames, render.bodyText
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type
	var view string
	if compilerTypes.IsInlineString(sourceType) {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
			return err
		}
		view = "hex_text_inline(&" + loop + ")"
	} else {
		if err := renderInto(body, "module.c", "const_ptr_decl", forStmtLineModel{Indent: indent, Type: "hex_string", Name: loop, Value: source}); err != nil {
			return err
		}
		view = "hex_text_heap(" + loop + ")"
	}

	cursorVariable := loop + "_cursor"
	if err := renderInto(body, "module.c", "grapheme_cursor_init", forStmtLineModel{Indent: indent, Name: cursorVariable, Value: view}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "grapheme_while_open", forStmtLineModel{Indent: indent, Name: cursorVariable}); err != nil {
		return err
	}
	if err := writeLineDirective(body, state.line(statement.Binders[0].Span), state.filename); err != nil {
		return err
	}
	if len(statement.Binders) == 2 {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent + "    ", Type: "size_t", Name: binderNames[0], Value: cursorVariable + ".offset"}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "grapheme_next_decl", forStmtLineModel{
		Indent: indent,
		Name:   binderNames[len(binderNames)-1],
		Value:  cursorVariable,
	}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: bodyText.String()}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

// renderForDict lowers Dict iteration to a bucket scan plus a separate
// produced-entry ordinal. The ordinal is pre-incremented so a body
// `continue` never skips it, and the public index never exposes the bucket.
func renderForDict(body *strings.Builder, statement checker.ForStatement, render forLoopRender, state *expressionValidation, indent string) error {
	loop, binderNames, bodyText := render.loop, render.binderNames, render.bodyText
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type

	if err := renderInto(body, "module.c", "const_ptr_decl", forStmtLineModel{Indent: indent, Type: sourceType.CName, Name: loop, Value: source}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "version_shadow", forStmtLineModel{Indent: indent, Name: loop}); err != nil {
		return err
	}
	bucketVariable := loop + "_bucket"
	ordinalVariable := loop + "_ordinal"
	hasIndex := len(statement.Binders) == 3
	keyType := statement.Binders[len(statement.Binders)-2].Type
	valueType := statement.Binders[len(statement.Binders)-1].Type

	if hasIndex {
		if err := renderInto(body, "module.c", "ordinal_decl", forStmtLineModel{Indent: indent, Name: ordinalVariable, Value: "(size_t)-1"}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "for_index_open", forOpenModel{Indent: indent, Var: bucketVariable, Limit: loop + "->capacity"}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "version_guard_open", forStmtLineModel{Indent: indent, Name: loop}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "runtime_trap", indentModel{Indent: indent}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "inner_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "bucket_active_open", forOpenModel{Indent: indent, Var: bucketVariable, Dict: loop}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "continue_stmt", indentModel{Indent: indent + "        "}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "inner_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	if hasIndex {
		if err := renderInto(body, "module.c", "ordinal_preinc", forStmtLineModel{Indent: indent, Name: ordinalVariable}); err != nil {
			return err
		}
	}
	if err := writeLineDirective(body, state.line(statement.Binders[0].Span), state.filename); err != nil {
		return err
	}
	if hasIndex {
		if err := renderInto(body, "module.c", "const_decl", forStmtLineModel{Indent: indent + "    ", Type: "size_t", Name: binderNames[0], Value: ordinalVariable}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "bucket_bind", bucketBindModel{
		Indent: indent,
		Target: declaration(keyType, binderNames[len(binderNames)-2], false),
		Dict:   loop,
		Var:    bucketVariable,
		Field:  "key",
	}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "bucket_bind", bucketBindModel{
		Indent: indent,
		Target: declaration(valueType, binderNames[len(binderNames)-1], false),
		Dict:   loop,
		Var:    bucketVariable,
		Field:  "value",
	}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: bodyText.String()}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}
