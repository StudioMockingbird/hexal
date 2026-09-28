package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func validatePipeline(pipeline *checker.Pipeline, state *expressionValidation) error {
	if pipeline == nil || pipeline.Source.Type != pipeline.SourceType ||
		pipeline.SourceType.List == nil && pipeline.SourceType.InlineList == nil && pipeline.SourceType.Slice == nil && pipeline.SourceType.Dict == nil ||
		!validateGeneratedType(pipeline.Element, state.generatedTypes, false) {
		return unknownExpressionDiagnostic()
	}
	current := pipeline.SourceElement
	if current == (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic()
	}
	if pipeline.SourceType.Dict != nil {
		switch pipeline.Projection {
		case "keys":
			if !compilerTypes.Equal(current, pipeline.SourceType.Dict.Key) {
				return unknownExpressionDiagnostic()
			}
		case "values":
			if !compilerTypes.Equal(current, pipeline.SourceType.Dict.Value) {
				return unknownExpressionDiagnostic()
			}
		case "entries":
			if current.Object == nil || len(current.Object.Members) != 2 || current.Object.Members[0].Name != "key" || current.Object.Members[1].Name != "value" ||
				!compilerTypes.Equal(current.Object.Members[0].Type, pipeline.SourceType.Dict.Key) || !compilerTypes.Equal(current.Object.Members[1].Type, pipeline.SourceType.Dict.Value) {
				return unknownExpressionDiagnostic()
			}
		case "pairs":
			if len(pipeline.Adaptors) != 0 || !compilerTypes.Equal(current, pipeline.SourceType.Dict.Key) {
				return unknownExpressionDiagnostic()
			}
		default:
			return unknownExpressionDiagnostic()
		}
	} else if pipeline.Projection != "" || !compilerTypes.Equal(current, pipelineSourceElement(pipeline.SourceType)) {
		return unknownExpressionDiagnostic()
	}
	for _, adaptor := range pipeline.Adaptors {
		if adaptor.Name != "map" && adaptor.Name != "filter" || !compilerTypes.Equal(adaptor.Input, current) ||
			adaptor.Callback.Type.Signature == nil || adaptor.Callback.Type.Signature.Rest ||
			len(adaptor.Callback.Type.Signature.Parameters) != 1 ||
			!compilerTypes.Equal(adaptor.Callback.Type.Signature.Parameters[0], current) {
			return unknownExpressionDiagnostic()
		}
		if err := validateCheckedOperandWithState(adaptor.Callback, state); err != nil {
			return err
		}
		if adaptor.Name == "filter" {
			if adaptor.Callback.Type.Signature.Result == nil || !compilerTypes.Equal(*adaptor.Callback.Type.Signature.Result, compilerTypes.Bool) || !compilerTypes.Equal(adaptor.Output, current) {
				return unknownExpressionDiagnostic()
			}
		} else {
			if adaptor.Callback.Type.Signature.Result == nil || !compilerTypes.Equal(*adaptor.Callback.Type.Signature.Result, adaptor.Output) {
				return unknownExpressionDiagnostic()
			}
		}
		current = adaptor.Output
	}
	if pipeline.Projection == "pairs" {
		if !compilerTypes.Equal(pipeline.Element, pipeline.SourceType.Dict.Key) &&
			(pipeline.Element.Union == nil || !compilerTypes.ContainsUnionMember(pipeline.Element, pipeline.SourceType.Dict.Key) || !compilerTypes.ContainsUnionMember(pipeline.Element, pipeline.SourceType.Dict.Value)) {
			return unknownExpressionDiagnostic()
		}
	} else if !compilerTypes.Equal(current, pipeline.Element) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validatePipelineTerminal(terminal *checker.PipelineTerminal, state *expressionValidation) error {
	if terminal == nil || terminal.Pipeline == nil {
		return unknownExpressionDiagnostic()
	}
	if err := validatePipeline(terminal.Pipeline, state); err != nil {
		return err
	}
	switch terminal.Kind {
	case "to_list":
		if terminal.Heap == nil || terminal.Initial != nil || terminal.Combiner != nil || terminal.Result.List == nil ||
			!compilerTypes.Equal(terminal.Result.List.Element, terminal.Pipeline.Element) || !compilerTypes.IsHeap(terminal.Heap.Type) {
			return unknownExpressionDiagnostic()
		}
		return validateCheckedOperandWithState(*terminal.Heap, state)
	case "reduce":
		if terminal.Initial == nil || terminal.Combiner == nil || terminal.Heap != nil ||
			!compilerTypes.Equal(terminal.Initial.Type, terminal.Result) || terminal.Combiner.Type.Signature == nil ||
			terminal.Combiner.Type.Signature.Rest || len(terminal.Combiner.Type.Signature.Parameters) != 2 ||
			terminal.Combiner.Type.Signature.Result == nil ||
			!compilerTypes.Equal(terminal.Combiner.Type.Signature.Parameters[0], terminal.Result) ||
			!compilerTypes.Equal(terminal.Combiner.Type.Signature.Parameters[1], terminal.Pipeline.Element) ||
			!compilerTypes.Equal(*terminal.Combiner.Type.Signature.Result, terminal.Result) {
			return unknownExpressionDiagnostic()
		}
		if err := validateCheckedOperandWithState(*terminal.Initial, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(*terminal.Combiner, state)
	default:
		return unknownExpressionDiagnostic()
	}
}

func renderPipelineForStatement(body *strings.Builder, statement checker.ForStatement, state *expressionValidation, result *compilerTypes.Type, inFunction bool, indent string) error {
	pipeline := statement.Pipeline
	if pipeline == nil || len(statement.Binders) != 1 {
		return unknownExpressionDiagnostic()
	}
	if pipeline.SourceType.List == nil && pipeline.SourceType.InlineList == nil && pipeline.SourceType.Slice == nil && pipeline.SourceType.Dict == nil {
		return unknownExpressionDiagnostic()
	}
	state.loopCounter++
	loop := fmt.Sprintf("hex_pipeline_%d", state.loopCounter)
	source, err := renderOperandWithState(pipeline.Source, state)
	if err != nil {
		return err
	}
	var out strings.Builder
	if err := writeLineDirective(&out, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	sourceName := loop + "_source"
	indexName := loop + "_index"
	length := ""
	access := ""
	versioned := pipeline.SourceType.List != nil || pipeline.SourceType.InlineList != nil || pipeline.SourceType.Dict != nil
	switch {
	case pipeline.SourceType.List != nil:
		fmt.Fprintf(&out, "%sconst %s *const %s = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
		length = sourceName + "->length"
		access = fmt.Sprintf("*hex_list_at_%s(%s, %s)", listSuffix(pipeline.SourceType), sourceName, indexName)
	case pipeline.SourceType.InlineList != nil:
		if pipeline.Source.Addressable {
			fmt.Fprintf(&out, "%sconst %s *const %s = &(%s);\n", indent, pipeline.SourceType.CName, sourceName, source)
		} else {
			fmt.Fprintf(&out, "%sconst %s %s_value = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
			fmt.Fprintf(&out, "%sconst %s *const %s = &%s_value;\n", indent, pipeline.SourceType.CName, sourceName, sourceName)
		}
		length = sourceName + "->length"
		access = fmt.Sprintf("*hex_list_inline_at_%s(%s, %s)", listSuffix(pipeline.SourceType), sourceName, indexName)
	case pipeline.SourceType.Slice != nil:
		fmt.Fprintf(&out, "%sconst %s %s = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
		length = sourceName + ".length"
		access = fmt.Sprintf("*%s(%s, %s)", sliceAtHelper(pipeline.SourceType), sourceName, indexName)
	case pipeline.SourceType.Dict != nil:
		fmt.Fprintf(&out, "%sconst %s *const %s = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
		length = sourceName + "->capacity"
		bucket := fmt.Sprintf("%s->buckets[%s]", sourceName, indexName)
		switch pipeline.Projection {
		case "keys":
			access = bucket + ".key"
		case "values":
			access = bucket + ".value"
		case "entries":
			access = fmt.Sprintf("(%s){ .hex_m_key = %s.key, .hex_m_value = %s.value }", pipeline.SourceElement.CName, bucket, bucket)
		default:
			return unknownExpressionDiagnostic()
		}
	}
	if versioned {
		fmt.Fprintf(&out, "%sconst size_t %s_version = %s->version;\n", indent, sourceName, sourceName)
	}
	callbacks := make([]string, len(pipeline.Adaptors))
	for index, adaptor := range pipeline.Adaptors {
		callbacks[index] = fmt.Sprintf("%s_callback_%d", loop, index)
		value, renderErr := renderOperandWithState(adaptor.Callback, state)
		if renderErr != nil {
			return renderErr
		}
		fmt.Fprintf(&out, "%s%s = %s;\n", indent, declaration(adaptor.Callback.Type, callbacks[index], false), value)
	}
	if versioned {
		fmt.Fprintf(&out, "%sfor (size_t %s = 0; %s < %s; %s++, (%s->version != %s_version ? hex_runtime_trap(\"[Runtime Error] collection modified during iteration\\n\") : (void)0)) {\n", indent, indexName, indexName, length, indexName, sourceName, sourceName)
		fmt.Fprintf(&out, "%s    if (%s->version != %s_version) {\n", indent, sourceName, sourceName)
		fmt.Fprintf(&out, "%s        hex_runtime_trap(\"[Runtime Error] collection modified during iteration\\n\");\n", indent)
		fmt.Fprintf(&out, "%s    }\n", indent)
	} else {
		fmt.Fprintf(&out, "%sfor (size_t %s = 0; %s < %s; %s++) {\n", indent, indexName, indexName, length, indexName)
	}
	if pipeline.SourceType.Dict != nil {
		fmt.Fprintf(&out, "%s    if (!%s->buckets[%s].active) {\n", indent, sourceName, indexName)
		fmt.Fprintf(&out, "%s        continue;\n", indent)
		fmt.Fprintf(&out, "%s    }\n", indent)
	}
	valueName := loop + "_value"
	sourceElement := pipeline.SourceElement
	if sourceElement == (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic()
	}
	innerIndent := indent + "    "
	fmt.Fprintf(&out, "%sconst %s %s = %s;\n", innerIndent, sourceElement.CName, valueName, access)
	current := valueName
	currentType := sourceElement
	activeIndent := innerIndent
	for index, adaptor := range pipeline.Adaptors {
		if !compilerTypes.Equal(adaptor.Input, currentType) {
			return unknownExpressionDiagnostic()
		}
		call := callbacks[index] + "(" + current + ")"
		if adaptor.Name == "filter" {
			fmt.Fprintf(&out, "%sif (%s) {\n", activeIndent, call)
			activeIndent += "    "
			continue
		}
		if adaptor.Name != "map" {
			return unknownExpressionDiagnostic()
		}
		next := fmt.Sprintf("%s_mapped_%d", loop, index)
		fmt.Fprintf(&out, "%s%s = %s;\n", activeIndent, declaration(adaptor.Output, next, false), call)
		current, currentType = next, adaptor.Output
	}
	if !compilerTypes.Equal(statement.Binders[0].Type, pipeline.Element) || !compilerTypes.Equal(currentType, pipeline.Element) {
		return unknownExpressionDiagnostic()
	}
	if err := writeLineDirective(&out, state.line(statement.Binders[0].Span), state.filename); err != nil {
		return err
	}
	state.pushScope()
	binder, err := state.allocateBinding(statement.Binders[0].Binding, statement.Binders[0].Name, statement.Binders[0].Type, false)
	if err != nil {
		return err
	}
	fmt.Fprintf(&out, "%s%s = %s;\n", activeIndent, declaration(statement.Binders[0].Type, binder, false), current)
	previousLoopDepth := state.loopDepth
	state.loopDepth++
	state.loopDepths = append(state.loopDepths, len(state.deferStack))
	var loopBody strings.Builder
	err = writeStatementsAt(&loopBody, statement.Body, state, statementFrame{result: result, inFunction: inFunction, defers: statement.BodyDefers}, activeIndent+"    ")
	state.loopDepths = state.loopDepths[:len(state.loopDepths)-1]
	state.loopDepth = previousLoopDepth
	if popErr := state.popScope(); popErr != nil {
		return popErr
	}
	if err != nil {
		return err
	}
	out.WriteString(loopBody.String())
	for index := len(pipeline.Adaptors) - 1; index >= 0; index-- {
		if pipeline.Adaptors[index].Name == "filter" {
			activeIndent = activeIndent[:len(activeIndent)-4]
			fmt.Fprintf(&out, "%s}\n", activeIndent)
		}
	}
	fmt.Fprintf(&out, "%s}\n", indent)
	body.WriteString(out.String())
	return nil
}

func pipelineSourceElement(typ compilerTypes.Type) compilerTypes.Type {
	switch {
	case typ.List != nil:
		return typ.List.Element
	case typ.InlineList != nil:
		return typ.InlineList.Element
	case typ.Slice != nil:
		return typ.Slice.Element
	default:
		return compilerTypes.Type{}
	}
}

func renderPipelineTerminalValue(body *strings.Builder, terminal *checker.PipelineTerminal, state *expressionValidation, indent string) (string, error) {
	if err := validatePipelineTerminal(terminal, state); err != nil {
		return "", err
	}
	pipeline := terminal.Pipeline
	if pipeline.SourceType.List == nil && pipeline.SourceType.InlineList == nil && pipeline.SourceType.Slice == nil && pipeline.SourceType.Dict == nil {
		return "", unknownExpressionDiagnostic()
	}
	state.loopCounter++
	loop := fmt.Sprintf("hex_pipeline_%d", state.loopCounter)
	source, err := renderOperandWithState(pipeline.Source, state)
	if err != nil {
		return "", err
	}
	if err := writeLineDirective(body, state.line(pipeline.Source.Node.Span), state.filename); err != nil {
		return "", err
	}
	sourceName := loop + "_source"
	indexName := loop + "_index"
	length, access := "", ""
	versioned := pipeline.SourceType.List != nil || pipeline.SourceType.InlineList != nil || pipeline.SourceType.Dict != nil
	switch {
	case pipeline.SourceType.List != nil:
		fmt.Fprintf(body, "%sconst %s *const %s = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
		length = sourceName + "->length"
		access = fmt.Sprintf("*hex_list_at_%s(%s, %s)", listSuffix(pipeline.SourceType), sourceName, indexName)
	case pipeline.SourceType.InlineList != nil:
		if pipeline.Source.Addressable {
			fmt.Fprintf(body, "%sconst %s *const %s = &(%s);\n", indent, pipeline.SourceType.CName, sourceName, source)
		} else {
			fmt.Fprintf(body, "%sconst %s %s_value = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
			fmt.Fprintf(body, "%sconst %s *const %s = &%s_value;\n", indent, pipeline.SourceType.CName, sourceName, sourceName)
		}
		length = sourceName + "->length"
		access = fmt.Sprintf("*hex_list_inline_at_%s(%s, %s)", listSuffix(pipeline.SourceType), sourceName, indexName)
	case pipeline.SourceType.Slice != nil:
		fmt.Fprintf(body, "%sconst %s %s = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
		length = sourceName + ".length"
		access = fmt.Sprintf("*%s(%s, %s)", sliceAtHelper(pipeline.SourceType), sourceName, indexName)
	case pipeline.SourceType.Dict != nil:
		fmt.Fprintf(body, "%sconst %s *const %s = %s;\n", indent, pipeline.SourceType.CName, sourceName, source)
		length = sourceName + "->capacity"
		bucket := fmt.Sprintf("%s->buckets[%s]", sourceName, indexName)
		switch pipeline.Projection {
		case "keys":
			access = bucket + ".key"
		case "values":
			access = bucket + ".value"
		case "entries":
			access = fmt.Sprintf("(%s){ .hex_m_key = %s.key, .hex_m_value = %s.value }", pipeline.SourceElement.CName, bucket, bucket)
		case "pairs":
			access = bucket + ".key"
		default:
			return "", unknownExpressionDiagnostic()
		}
	}
	if versioned {
		fmt.Fprintf(body, "%sconst size_t %s_version = %s->version;\n", indent, sourceName, sourceName)
	}
	callbacks := make([]string, len(pipeline.Adaptors))
	for index, adaptor := range pipeline.Adaptors {
		callbacks[index] = fmt.Sprintf("%s_callback_%d", loop, index)
		value, renderErr := renderOperandWithState(adaptor.Callback, state)
		if renderErr != nil {
			return "", renderErr
		}
		fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(adaptor.Callback.Type, callbacks[index], false), value)
	}
	result := loop + "_result"
	combiner := ""
	if terminal.Kind == "to_list" {
		heap, renderErr := renderOperandWithState(*terminal.Heap, state)
		if renderErr != nil {
			return "", renderErr
		}
		fmt.Fprintf(body, "%s%s = hex_list_new_%s(%s);\n", indent, declaration(terminal.Result, result, false), listSuffix(terminal.Result), heap)
	} else {
		initial, renderErr := renderOperandWithState(*terminal.Initial, state)
		if renderErr != nil {
			return "", renderErr
		}
		combiner, renderErr = renderOperandWithState(*terminal.Combiner, state)
		if renderErr != nil {
			return "", renderErr
		}
		fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(terminal.Result, result, true), initial)
		combinerName := loop + "_combiner"
		fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(terminal.Combiner.Type, combinerName, false), combiner)
		combiner = combinerName
	}
	if versioned {
		fmt.Fprintf(body, "%sfor (size_t %s = 0; %s < %s; %s++, (%s->version != %s_version ? hex_runtime_trap(\"[Runtime Error] collection modified during iteration\\n\") : (void)0)) {\n", indent, indexName, indexName, length, indexName, sourceName, sourceName)
		fmt.Fprintf(body, "%s    if (%s->version != %s_version) {\n", indent, sourceName, sourceName)
		fmt.Fprintf(body, "%s        hex_runtime_trap(\"[Runtime Error] collection modified during iteration\\n\");\n", indent)
		fmt.Fprintf(body, "%s    }\n", indent)
	} else {
		fmt.Fprintf(body, "%sfor (size_t %s = 0; %s < %s; %s++) {\n", indent, indexName, indexName, length, indexName)
	}
	if pipeline.SourceType.Dict != nil {
		fmt.Fprintf(body, "%s    if (!%s->buckets[%s].active) {\n", indent, sourceName, indexName)
		fmt.Fprintf(body, "%s        continue;\n", indent)
		fmt.Fprintf(body, "%s    }\n", indent)
	}
	innerIndent := indent + "    "
	element := pipeline.SourceElement
	current := loop + "_value"
	fmt.Fprintf(body, "%s%s = %s;\n", innerIndent, declaration(element, current, false), access)
	activeIndent, currentType := innerIndent, element
	for index, adaptor := range pipeline.Adaptors {
		if !compilerTypes.Equal(adaptor.Input, currentType) {
			return "", unknownExpressionDiagnostic()
		}
		call := callbacks[index] + "(" + current + ")"
		if adaptor.Name == "filter" {
			fmt.Fprintf(body, "%sif (%s) {\n", activeIndent, call)
			activeIndent += "    "
			continue
		}
		mapped := fmt.Sprintf("%s_mapped_%d", loop, index)
		fmt.Fprintf(body, "%s%s = %s;\n", activeIndent, declaration(adaptor.Output, mapped, false), call)
		current, currentType = mapped, adaptor.Output
	}
	if terminal.Kind == "to_list" {
		if pipeline.Projection == "pairs" {
			key := fmt.Sprintf("%s->buckets[%s].key", sourceName, indexName)
			value := fmt.Sprintf("%s->buckets[%s].value", sourceName, indexName)
			if compilerTypes.Equal(pipeline.SourceType.Dict.Key, pipeline.SourceType.Dict.Value) {
				fmt.Fprintf(body, "%shex_list_push_%s(%s, %s);\n", activeIndent, listSuffix(terminal.Result), result, key)
				fmt.Fprintf(body, "%shex_list_push_%s(%s, %s);\n", activeIndent, listSuffix(terminal.Result), result, value)
			} else {
				union := terminal.Result.List.Element
				for _, item := range []struct {
					typ   compilerTypes.Type
					value string
				}{{pipeline.SourceType.Dict.Key, key}, {pipeline.SourceType.Dict.Value, value}} {
					fmt.Fprintf(body, "%shex_list_push_%s(%s, (%s){ .tag = %s, .payload.%s = %s });\n", activeIndent,
						listSuffix(terminal.Result), result, union.CName, state.tags.unionMemberTag(item.typ), state.tags.unionPayloadField(item.typ), item.value)
				}
			}
		} else {
			fmt.Fprintf(body, "%shex_list_push_%s(%s, %s);\n", activeIndent, listSuffix(terminal.Result), result, current)
		}
	} else {
		fmt.Fprintf(body, "%s%s = %s(%s, %s);\n", activeIndent, result, combiner, result, current)
	}
	for index := len(pipeline.Adaptors) - 1; index >= 0; index-- {
		if pipeline.Adaptors[index].Name == "filter" {
			activeIndent = activeIndent[:len(activeIndent)-4]
			fmt.Fprintf(body, "%s}\n", activeIndent)
		}
	}
	fmt.Fprintf(body, "%s}\n", indent)
	return result, nil
}
