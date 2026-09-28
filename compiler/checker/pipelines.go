package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func checkPipelineMethodCall(call parser.CallExpression, callee parser.PropertyExpression, ctx checkContext) (checkedExpression, bool) {
	name := callee.Property.Lexeme
	switch name {
	case "map", "filter", "keys", "values", "entries", "to_list", "reduce":
	default:
		return checkedExpression{}, false
	}
	receiver := checkPipelineReceiver(callee.Receiver, ctx)
	if diagnostics := initializerDiagnostics(receiver); len(diagnostics) > 0 {
		return receiver, true
	}
	if receiver.pipeline != nil {
		switch name {
		case "map", "filter":
			return addPipelineAdaptor(receiver.pipeline, call, callee.Property, ctx), true
		case "to_list":
			return checkPipelineToList(receiver.pipeline, call, callee.Property, ctx), true
		case "reduce":
			return checkPipelineReduce(receiver.pipeline, call, callee.Property, ctx), true
		default:
			diagnostic := messageAt(callee.Property, diag.LazyPipelineMustBeConsumed())
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}, true
		}
	}
	if name == "to_list" && receiver.typ.Slice != nil {
		pipeline := newPipeline(receiver.source, receiver.typ, receiver.typ.Slice.Element, ctx.names)
		return checkPipelineToList(pipeline, call, callee.Property, ctx), true
	}
	if receiver.typ.Dict != nil {
		switch name {
		case "keys", "values", "entries":
			if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
				diagnostic := messageAt(callee.Property, diag.CollectionMethodNoArguments(name))
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}, true
			}
			element := receiver.typ.Dict.Key
			if name == "values" {
				element = receiver.typ.Dict.Value
			} else if name == "entries" {
				element = ctx.typeEnvironment.DictEntryType(receiver.typ.Dict.Key, receiver.typ.Dict.Value)
			}
			pipeline := newPipeline(receiver.source, receiver.typ, element, ctx.names)
			pipeline.Projection = name
			return checkedExpression{pipeline: pipeline, token: callee.Property}, true
		case "to_list":
			union := receiver.typ.Dict.Key
			if !compilerTypes.Equal(receiver.typ.Dict.Key, receiver.typ.Dict.Value) {
				union = ctx.typeEnvironment.UnionType([]compilerTypes.Type{receiver.typ.Dict.Key, receiver.typ.Dict.Value})
			}
			if union == (compilerTypes.Type{}) {
				diagnostic := messageAt(callee.Property, diag.InvalidListElementType("Dict key/value union"))
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}, true
			}
			pipeline := newPipeline(receiver.source, receiver.typ, receiver.typ.Dict.Key, ctx.names)
			pipeline.Element = union
			pipeline.Projection = "pairs"
			return checkPipelineToList(pipeline, call, callee.Property, ctx), true
		}
	}
	if name == "map" || name == "filter" {
		element, supported := pipelineSourceElement(receiver.typ)
		if !supported {
			return checkedExpression{}, false
		}
		pipeline := newPipeline(receiver.source, receiver.typ, element, ctx.names)
		return addPipelineAdaptor(pipeline, call, callee.Property, ctx), true
	}
	return checkedExpression{}, false
}

func newPipeline(source Operand, sourceType, element compilerTypes.Type, names *scope) *Pipeline {
	root := BindingID(0)
	if binding := baseBindingID(&source.Node); binding != 0 {
		root = collectionRootForOperand(source, names, binding)
	}
	return &Pipeline{Source: source, SourceType: sourceType, SourceElement: element, Element: element, CollectionRoot: root}
}

func checkPipelineToList(pipeline *Pipeline, call parser.CallExpression, token lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 1 || len(call.ArgumentLabels) != 1 || call.ArgumentLabels[0] != nil || len(call.TypeArguments) != 0 {
		diagnostic := messageAt(token, diag.LazyPipelineMustBeConsumed())
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := messageAt(heap.token, diag.ListConstructorHeapType(heap.typ.Name))
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	list := ctx.typeEnvironment.ListType(pipeline.Element)
	if list == (compilerTypes.Type{}) {
		diagnostic := messageAt(token, diag.InvalidListElementType(pipeline.Element.Name))
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	heapSource := heap.source
	terminal := &PipelineTerminal{Pipeline: pipeline, Kind: "to_list", Heap: &heapSource, Result: list}
	return checkedExpression{terminal: terminal, typ: list, use: compilerTypes.NewTypeUse(list), token: token}
}

func checkPipelineReduce(pipeline *Pipeline, call parser.CallExpression, token lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 2 || len(call.ArgumentLabels) != 2 || call.ArgumentLabels[0] != nil || call.ArgumentLabels[1] != nil || len(call.TypeArguments) != 0 {
		diagnostic := messageAt(token, diag.LazyPipelineCallbackSignature())
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	combiner := checkExpression(call.Arguments[1], expressionContext{}, ctx)
	if diagnostics := initializerDiagnostics(combiner); len(diagnostics) > 0 {
		return combiner
	}
	signature := combiner.typ.Signature
	if signature == nil || signature.Rest || len(signature.Parameters) != 2 || signature.Result == nil ||
		compilerTypes.ContainsTypeParameter(combiner.typ) || compilerTypes.ContainsTypeParameter(signature.Parameters[0]) ||
		compilerTypes.ContainsTypeParameter(signature.Parameters[1]) || compilerTypes.ContainsTypeParameter(*signature.Result) ||
		!compilerTypes.Equal(signature.Parameters[0], *signature.Result) || !compilerTypes.Equal(signature.Parameters[1], pipeline.Element) {
		diagnostic := messageAt(token, diag.LazyPipelineCallbackSignature())
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	initial := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(signature.Parameters[0]), token, ctx)
	if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
		return initial
	}
	initialSource := initial.source
	combinerSource := combiner.source
	terminal := &PipelineTerminal{Pipeline: pipeline, Kind: "reduce", Initial: &initialSource, Combiner: &combinerSource, Result: *signature.Result}
	return checkedExpression{terminal: terminal, typ: *signature.Result, use: compilerTypes.NewTypeUse(*signature.Result), token: token}
}

func checkPipelineReceiver(expression parser.Expression, ctx checkContext) checkedExpression {
	switch expression.(type) {
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		return checkPlace(expression, ctx)
	default:
		return checkExpression(expression, expressionContext{}, ctx)
	}
}

func pipelineSourceElement(typ compilerTypes.Type) (compilerTypes.Type, bool) {
	switch {
	case typ.List != nil:
		return typ.List.Element, true
	case typ.InlineList != nil:
		return typ.InlineList.Element, true
	case typ.Slice != nil:
		return typ.Slice.Element, true
	default:
		return compilerTypes.Type{}, false
	}
}

func addPipelineAdaptor(pipeline *Pipeline, call parser.CallExpression, token lexer.Token, ctx checkContext) checkedExpression {
	name := "map"
	if property, ok := call.Callee.(parser.PropertyExpression); ok {
		name = property.Property.Lexeme
	}
	if len(call.Arguments) != 1 || len(call.ArgumentLabels) != 1 || call.ArgumentLabels[0] != nil || len(call.TypeArguments) != 0 {
		diagnostic := messageAt(token, diag.LazyPipelineCallbackSignature())
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	callback := checkExpression(call.Arguments[0], expressionContext{}, ctx)
	if diagnostics := initializerDiagnostics(callback); len(diagnostics) > 0 {
		return callback
	}
	signature := callback.typ.Signature
	if signature == nil || signature.Rest || len(signature.Parameters) != 1 ||
		compilerTypes.ContainsTypeParameter(callback.typ) ||
		compilerTypes.ContainsTypeParameter(signature.Parameters[0]) ||
		(signature.Result != nil && compilerTypes.ContainsTypeParameter(*signature.Result)) {
		diagnostic := messageAt(token, diag.LazyPipelineCallbackSignature())
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	if !compilerTypes.Equal(signature.Parameters[0], pipeline.Element) || signature.Result == nil {
		diagnostic := messageAt(token, diag.LazyPipelineCallbackSignature())
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	output := *signature.Result
	if name == "filter" {
		if !compilerTypes.Equal(output, compilerTypes.Bool) {
			diagnostic := messageAt(token, diag.LazyPipelineCallbackSignature())
			return checkedExpression{token: token, diagnostic: &diagnostic}
		}
		output = pipeline.Element
	}
	copy := *pipeline
	copy.Adaptors = append(append([]PipelineAdaptor(nil), pipeline.Adaptors...), PipelineAdaptor{
		Name: name, Callback: callback.source, Input: pipeline.Element, Output: output,
	})
	copy.Element = output
	return checkedExpression{pipeline: &copy, token: token}
}
