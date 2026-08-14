package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// renderForStatement lowers the RFC 0028 for-in form. The source is
// stabilized exactly once: Array places iterate in place through their
// address, temporary Arrays and Strands are materialized into one inline
// copy, and String, List, and Dict sources copy their pointer-sized handle.
//
// Index semantics: Array, View, and List bind the Size loop counter directly
// (a body `continue` lands on the loop increment). String, Strand, and Dict
// loops pre-increment their produced-entry ordinal before the body, so a
// body `continue` never skips the increment, matching RFC 0028.
func renderForStatement(body *strings.Builder, statement checker.ForStatement, state *expressionValidation, result *compilerTypes.Type, inFunction bool, indent string) error {
	sourceType := statement.Source.Type
	writeLineDirective(body, statement.SourceLine, state.filename)

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
	err := writeStatementsAt(&bodyText, statement.Body, state, result, inFunction, indent+"    ", statement.BodyDefers)
	state.loopDepths = state.loopDepths[:len(state.loopDepths)-1]
	state.loopDepth = previousLoopDepth
	state.popScope()
	if err != nil {
		return err
	}

	switch {
	case sourceType.Array != nil:
		return renderForSequence(body, statement, loop, binderNames, &bodyText, state, indent)
	case sourceType.View != nil:
		return renderForSequence(body, statement, loop, binderNames, &bodyText, state, indent)
	case sourceType.List != nil:
		return renderForSequence(body, statement, loop, binderNames, &bodyText, state, indent)
	case compilerTypes.IsString(sourceType):
		return renderForText(body, statement, loop, binderNames, &bodyText, state, indent)
	case compilerTypes.IsStrand(sourceType):
		return renderForText(body, statement, loop, binderNames, &bodyText, state, indent)
	case sourceType.Stream != nil:
		return renderForStream(body, statement, loop, binderNames, &bodyText, state, indent)
	case sourceType.Dict != nil:
		return renderForDict(body, statement, loop, binderNames, &bodyText, state, indent)
	default:
		return unknownExpressionDiagnostic("unsupported for-in source type " + sourceType.Name)
	}
}

// renderForSequence lowers Array, View, and List iteration to a plain index
// loop over the captured source.
func renderForSequence(body *strings.Builder, statement checker.ForStatement, loop string, binderNames []string, bodyText *strings.Builder, state *expressionValidation, indent string) error {
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
			fmt.Fprintf(body, "%sconst %s *const %s = &(%s);\n", indent, sourceType.CName, loop, source)
		} else {
			fmt.Fprintf(body, "%sconst %s %s = %s;\n", indent, sourceType.CName, loop, source)
		}
		length = fmt.Sprintf("(size_t)(%d)", sourceType.Array.Length)
		elementAccess = fmt.Sprintf("*hex_array_at_%s(%s, (size_t)(%s_index))", arrayAccessorSuffix(sourceType), loop, loop)
	case sourceType.View != nil:
		fmt.Fprintf(body, "%sconst %s %s = %s;\n", indent, sourceType.CName, loop, source)
		length = fmt.Sprintf("%s.length", loop)
		elementAccess = fmt.Sprintf("*hex_view_at_%s(%s, (size_t)(%s_index))", strings.TrimPrefix(sourceType.CName, "hex_view_"), loop, loop)
	case sourceType.List != nil:
		fmt.Fprintf(body, "%sconst %s *const %s = %s;\n", indent, sourceType.CName, loop, source)
		length = fmt.Sprintf("%s->length", loop)
		elementAccess = fmt.Sprintf("*hex_list_at_%s(%s, (size_t)(%s_index))", listSuffix(sourceType), loop, loop)
	default:
		return unknownExpressionDiagnostic("unknown for-in sequence kind")
	}

	indexVariable := loop + "_index"
	fmt.Fprintf(body, "%sfor (size_t %s = 0; %s < %s; %s++) {\n", indent, indexVariable, indexVariable, length, indexVariable)
	writeLineDirective(body, statement.Binders[0].SourceLine, state.filename)
	valueBinder := statement.Binders[len(statement.Binders)-1]
	if len(statement.Binders) == 2 {
		fmt.Fprintf(body, "%s    const size_t %s = %s;\n", indent, binderNames[0], indexVariable)
	}
	fmt.Fprintf(body, "%s    const %s %s = %s;\n", indent, valueBinder.Type.CName, binderNames[len(binderNames)-1], elementAccess)
	body.WriteString(bodyText.String())
	fmt.Fprintf(body, "%s}\n", indent)
	return nil
}

// renderForText lowers String and Strand iteration to a sequential UTF-8
// cursor loop producing decoded Rune values. The produced-entry ordinal is
// pre-incremented so a body `continue` never skips it.
func renderForText(body *strings.Builder, statement checker.ForStatement, loop string, binderNames []string, bodyText *strings.Builder, state *expressionValidation, indent string) error {
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type
	if compilerTypes.IsStrand(sourceType) {
		fmt.Fprintf(body, "%sconst %s %s = %s;\n", indent, sourceType.CName, loop, source)
	} else {
		fmt.Fprintf(body, "%sconst %s *const %s = %s;\n", indent, sourceType.CName, loop, source)
	}

	offsetVariable := loop + "_offset"
	ordinalVariable := loop + "_ordinal"
	runeVariable := loop + "_rune"
	hasIndex := len(statement.Binders) == 2
	byteLength := fmt.Sprintf("%s.byte_length", loop)
	data := fmt.Sprintf("%s.data", loop)
	if !compilerTypes.IsStrand(sourceType) {
		byteLength = fmt.Sprintf("%s->byte_length", loop)
		data = fmt.Sprintf("%s->data", loop)
	}

	fmt.Fprintf(body, "%ssize_t %s = 0;\n", indent, offsetVariable)
	if hasIndex {
		fmt.Fprintf(body, "%ssize_t %s = (size_t)-1;\n", indent, ordinalVariable)
	}
	fmt.Fprintf(body, "%swhile (%s < %s) {\n", indent, offsetVariable, byteLength)
	fmt.Fprintf(body, "%s    uint64_t %s = hex_utf8_next(%s, %s, &%s);\n", indent, runeVariable, data, byteLength, offsetVariable)
	if hasIndex {
		fmt.Fprintf(body, "%s    %s++;\n", indent, ordinalVariable)
	}
	writeLineDirective(body, statement.Binders[0].SourceLine, state.filename)
	valueBinder := statement.Binders[len(statement.Binders)-1]
	if hasIndex {
		fmt.Fprintf(body, "%s    const size_t %s = %s;\n", indent, binderNames[0], ordinalVariable)
	}
	fmt.Fprintf(body, "%s    const %s %s = (%s);\n", indent, valueBinder.Type.CName, binderNames[len(binderNames)-1], runeVariable)
	body.WriteString(bodyText.String())
	fmt.Fprintf(body, "%s}\n", indent)
	return nil
}

// renderForStream lowers Stream<T> iteration to a plain pull loop through
// the internal next ABI. Unlike the finite collections it captures no
// traversal boundary: the loop ends when next returns false. The
// produced-entry ordinal is pre-incremented so a body `continue` never
// skips it (RFC 0031).
func renderForStream(body *strings.Builder, statement checker.ForStatement, loop string, binderNames []string, bodyText *strings.Builder, state *expressionValidation, indent string) error {
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type
	element := sourceType.Stream.Element
	valueVariable := loop + "_value"
	ordinalVariable := loop + "_ordinal"
	hasIndex := len(statement.Binders) == 2

	fmt.Fprintf(body, "%s%s *const %s = %s;\n", indent, sourceType.CName, loop, source)
	fmt.Fprintf(body, "%s%s %s;\n", indent, typeSpelling(element), valueVariable)
	if hasIndex {
		fmt.Fprintf(body, "%ssize_t %s = (size_t)-1;\n", indent, ordinalVariable)
	}
	fmt.Fprintf(body, "%swhile (%s->ops->next((void *)%s, &%s)) {\n", indent, loop, loop, valueVariable)
	if hasIndex {
		fmt.Fprintf(body, "%s    %s++;\n", indent, ordinalVariable)
	}
	writeLineDirective(body, statement.Binders[0].SourceLine, state.filename)
	valueBinder := statement.Binders[len(statement.Binders)-1]
	if hasIndex {
		fmt.Fprintf(body, "%s    const size_t %s = %s;\n", indent, binderNames[0], ordinalVariable)
	}
	fmt.Fprintf(body, "%s    const %s %s = %s;\n", indent, valueBinder.Type.CName, binderNames[len(binderNames)-1], valueVariable)
	body.WriteString(bodyText.String())
	fmt.Fprintf(body, "%s}\n", indent)
	return nil
}

// renderForDict lowers Dict iteration to a bucket scan plus a separate
// produced-entry ordinal. The ordinal is pre-incremented so a body
// `continue` never skips it, and the public index never exposes the bucket.
func renderForDict(body *strings.Builder, statement checker.ForStatement, loop string, binderNames []string, bodyText *strings.Builder, state *expressionValidation, indent string) error {
	source, err := renderOperandWithState(statement.Source, state)
	if err != nil {
		return err
	}
	sourceType := statement.Source.Type

	bucketVariable := loop + "_bucket"
	ordinalVariable := loop + "_ordinal"
	hasIndex := len(statement.Binders) == 3
	keyType := statement.Binders[len(statement.Binders)-2].Type
	valueType := statement.Binders[len(statement.Binders)-1].Type

	fmt.Fprintf(body, "%sconst %s *const %s = %s;\n", indent, sourceType.CName, loop, source)
	if hasIndex {
		fmt.Fprintf(body, "%ssize_t %s = (size_t)-1;\n", indent, ordinalVariable)
	}
	fmt.Fprintf(body, "%sfor (size_t %s = 0; %s < %s->capacity; %s++) {\n", indent, bucketVariable, bucketVariable, loop, bucketVariable)
	fmt.Fprintf(body, "%s    if (!%s->buckets[%s].active) {\n", indent, loop, bucketVariable)
	fmt.Fprintf(body, "%s        continue;\n", indent)
	fmt.Fprintf(body, "%s    }\n", indent)
	if hasIndex {
		fmt.Fprintf(body, "%s    %s++;\n", indent, ordinalVariable)
	}
	writeLineDirective(body, statement.Binders[0].SourceLine, state.filename)
	if hasIndex {
		fmt.Fprintf(body, "%s    const size_t %s = %s;\n", indent, binderNames[0], ordinalVariable)
	}
	fmt.Fprintf(body, "%s    const %s %s = %s->buckets[%s].key;\n", indent, keyType.CName, binderNames[len(binderNames)-2], loop, bucketVariable)
	fmt.Fprintf(body, "%s    const %s %s = %s->buckets[%s].value;\n", indent, valueType.CName, binderNames[len(binderNames)-1], loop, bucketVariable)
	body.WriteString(bodyText.String())
	fmt.Fprintf(body, "%s}\n", indent)
	return nil
}
