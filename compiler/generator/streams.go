package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0031: lazy single-pass pull Stream<T>. One pointer-sized handle names
// a fully heap-allocated combined header-and-state object; the canonical
// empty Stream allocates nothing. The internal pull ABI is
// bool next(void *object, T *out), dispatched through one immutable
// operations table per concrete source or adapter shape.

type generatedStreamState struct {
	order        []compilerTypes.Type
	produceNodes []streamProduceSpec
	listNodes    []compilerTypes.Type
	filterTypes  []compilerTypes.Type
	takeTypes    []compilerTypes.Type
	mapNodes     []streamMapSpec
	stepUnions   []streamStepUnion
}

type streamStepUnion struct {
	streamType compilerTypes.Type
	unionType  compilerTypes.Type
}

type streamProduceSpec struct {
	streamType   compilerTypes.Type
	stateType    compilerTypes.Type
	stepUnion    compilerTypes.Type
	callbackName string
}

type streamMapSpec struct {
	sourceType   compilerTypes.Type
	resultType   compilerTypes.Type
	callbackName string
}

func (state *generatedStreamState) add(stream compilerTypes.Type) {
	if state == nil || stream.Stream == nil {
		return
	}
	for _, existing := range state.order {
		if compilerTypes.Equal(existing, stream) {
			return
		}
	}
	state.order = append(state.order, stream)
}

func streamSuffix(stream compilerTypes.Type) string {
	return strings.TrimPrefix(stream.CName, "hex_stream_")
}

// discoverGeneratedStreams walks every checked statement and expression for
// Stream constructor and method nodes and records the stream types and node
// families the header must define.
func discoverGeneratedStreams(program checker.Program, moduleOwner string) (*generatedStreamState, error) {
	state := &generatedStreamState{}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.StreamConstructorExpression:
				state.add(node.OperandType)
				state.add(node.ResultType)
				if node.Name == "produce" && len(node.Arguments) == 3 {
					callback, err := streamCallbackCName(node.Arguments[2], moduleOwner)
					if err != nil {
						return err
					}
					stepUnion := compilerTypes.Type{}
					if node.Arguments[2].Type.Signature != nil && node.Arguments[2].Type.Signature.Result != nil {
						stepUnion = *node.Arguments[2].Type.Signature.Result
					}
					state.produceNodes = append(state.produceNodes, streamProduceSpec{
						streamType:   node.ResultType,
						stateType:    node.Arguments[1].Type,
						stepUnion:    stepUnion,
						callbackName: callback,
					})
				}
			case checker.StreamMethodCallExpression:
				state.add(node.OperandType)
				state.add(node.ResultType)
				switch node.Name {
				case "list_stream":
					state.listNodes = append(state.listNodes, node.OperandType)
				case "filter":
					state.filterTypes = append(state.filterTypes, node.ResultType)
				case "take":
					state.takeTypes = append(state.takeTypes, node.ResultType)
				case "next":
					state.stepUnions = append(state.stepUnions, streamStepUnion{streamType: node.OperandType, unionType: node.ResultType})
				case "map":
					if len(node.Arguments) == 2 {
						callback, err := streamCallbackCName(node.Arguments[1], moduleOwner)
						if err != nil {
							return err
						}
						state.mapNodes = append(state.mapNodes, streamMapSpec{
							sourceType:   node.OperandType,
							resultType:   node.ResultType,
							callbackName: callback,
						})
					}
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}
func writeStreamDefinitions(result *strings.Builder, streams *generatedStreamState) {
	if streams == nil || len(streams.order) == 0 {
		return
	}
	for _, stream := range streams.order {
		writeStreamBase(result, stream, streams)
	}
	for _, list := range streams.listNodes {
		writeStreamListFamily(result, list)
	}
	for _, stream := range streams.filterTypes {
		writeStreamFilterFamily(result, stream)
	}
	for _, stream := range streams.takeTypes {
		writeStreamTakeFamily(result, stream)
	}
	for _, spec := range streams.mapNodes {
		writeStreamMapFamily(result, spec)
	}
	for _, spec := range streams.produceNodes {
		writeStreamProduceFamily(result, spec)
	}
}

// writeStreamBase emits the ops table, canonical empty handle, and the
// public next and free helpers for one stream type.
func writeStreamBase(result *strings.Builder, stream compilerTypes.Type, streams *generatedStreamState) {
	element := stream.Stream.Element
	suffix := streamSuffix(stream)
	elementSpelling := typeSpelling(element)
	fmt.Fprintf(result, "\ntypedef struct hex_stream_ops_%s {\n    bool (*next)(void *object, %s *out);\n    void (*destroy)(void *object);\n} hex_stream_ops_%s;\n", suffix, elementSpelling, suffix)
	fmt.Fprintf(result, "typedef struct %s {\n    const hex_stream_ops_%s *ops;\n    uintptr_t allocator;\n    bool exhausted;\n} %s;\n", stream.CName, suffix, stream.CName)
	fmt.Fprintf(result, "static bool hex_stream_empty_next_%s(void *object, %s *out) {\n    (void)object;\n    (void)out;\n    return false;\n}\n", suffix, elementSpelling)
	fmt.Fprintf(result, "static void hex_stream_empty_destroy_%s(void *object) {\n    (void)object;\n}\n", suffix)
	fmt.Fprintf(result, "static const hex_stream_ops_%s hex_stream_empty_ops_%s = { hex_stream_empty_next_%s, hex_stream_empty_destroy_%s };\n", suffix, suffix, suffix, suffix)
	// The empty handle is deliberately not const: handle bindings render as
	// non-const-target pointers, and a const singleton would make every
	// reference discard the qualifier under -Werror.
	fmt.Fprintf(result, "static %s hex_stream_empty_%s = { &hex_stream_empty_ops_%s, 0, false };\n", stream.CName, suffix, suffix)

	stepUnion := compilerTypes.Type{}
	for _, record := range streams.stepUnions {
		if compilerTypes.Equal(record.streamType, stream) {
			stepUnion = record.unionType
			break
		}
	}
	if stepUnion == (compilerTypes.Type{}) {
		stepUnion = stepUnionFor(stream)
	}
	elementIndex := unionMemberIndex(stepUnion, element)
	eosIndex := unionMemberIndex(stepUnion, compilerTypes.EoS)
	if elementIndex >= 0 && eosIndex >= 0 {
		fmt.Fprintf(result, "\nstatic inline %s hex_stream_next_%s(%s *stream) {\n    %s step;\n    %s value;\n    if (stream->ops->next((void *)stream, &value)) {\n        step = (%s){ .tag = %s, .payload.member_%d = value };\n    } else {\n        step = (%s){ .tag = %s };\n    }\n    return step;\n}\n",
			stepUnion.CName, suffix, stream.CName, stepUnion.CName, elementSpelling, stepUnion.CName, unionTagName(stepUnion, elementIndex), elementIndex, stepUnion.CName, unionTagName(stepUnion, eosIndex))
	}
	fmt.Fprintf(result, "\nstatic inline void hex_stream_free_%s(hex_heap h, %s *stream) {\n    if (stream == NULL || stream == &hex_stream_empty_%s) {\n        return;\n    }\n    if (stream->allocator != h.identity) {\n        fputs(\"[Runtime Error] deallocation used the wrong allocator\\n\", stderr);\n        abort();\n    }\n    stream->ops->destroy((void *)stream);\n    hex_heap_free(stream, h.identity);\n}\n", suffix, stream.CName, suffix)
}

// stepUnionFor reconstructs the checked T | EoS union for one stream type
// by interning it in a fresh environment with the stream's element.
func stepUnionFor(stream compilerTypes.Type) compilerTypes.Type {
	environment := compilerTypes.NewEnvironment()
	return environment.UnionType([]compilerTypes.Type{stream.Stream.Element, compilerTypes.EoS})
}

// writeStreamListFamily emits the List<T>.stream(h) node: one node holding
// the List handle, the cursor, and the length captured at construction.
func writeStreamListFamily(result *strings.Builder, listType compilerTypes.Type) {
	element := listType.List.Element
	environment := compilerTypes.NewEnvironment()
	streamType := environment.StreamType(element)
	if streamType == (compilerTypes.Type{}) {
		return
	}
	suffix := streamSuffix(streamType)
	elementSpelling := typeSpelling(element)
	nodeName := "hex_stream_list_" + listSuffix(listType)
	fmt.Fprintf(result, "\ntypedef struct %s {\n    const hex_stream_ops_%s *ops;\n    uintptr_t allocator;\n    bool exhausted;\n    %s *list;\n    size_t index;\n    size_t length;\n} %s;\n\n", nodeName, suffix, listType.CName, nodeName)
	fmt.Fprintf(result, "static bool %s_next(void *object, %s *out) {\n    %s *node = (%s *)object;\n    if (node->index >= node->length) {\n        return false;\n    }\n    *out = *hex_list_at_%s(node->list, node->index);\n    node->index++;\n    return true;\n}\n", nodeName, elementSpelling, nodeName, nodeName, listSuffix(listType))
	fmt.Fprintf(result, "static void %s_destroy(void *object) {\n    (void)object;\n}\n", nodeName)
	fmt.Fprintf(result, "static const hex_stream_ops_%s %s_ops = { %s_next, %s_destroy };\n", suffix, nodeName, nodeName, nodeName)
	fmt.Fprintf(result, "static inline %s *%s_new(hex_heap h, %s *list) {\n    %s *node = hex_heap_raw_allocate(h.identity, sizeof(%s), _Alignof(%s));\n    node->ops = &%s_ops;\n    node->allocator = h.identity;\n    node->exhausted = false;\n    node->list = list;\n    node->index = 0;\n    node->length = list->length;\n    return (%s *)node;\n}\n", streamType.CName, nodeName, listType.CName, nodeName, nodeName, nodeName, nodeName, streamType.CName)
}

// writeStreamFilterFamily emits the filter adapter node: upstream handle and
// predicate function pointer.
func writeStreamFilterFamily(result *strings.Builder, stream compilerTypes.Type) {
	element := stream.Stream.Element
	suffix := streamSuffix(stream)
	elementSpelling := typeSpelling(element)
	nodeName := "hex_stream_filter_" + suffix
	fmt.Fprintf(result, "\ntypedef struct %s {\n    const hex_stream_ops_%s *ops;\n    uintptr_t allocator;\n    bool exhausted;\n    %s *upstream;\n    bool (*predicate)(%s);\n} %s;\n\n", nodeName, suffix, stream.CName, elementSpelling, nodeName)
	fmt.Fprintf(result, "static bool %s_next(void *object, %s *out) {\n    %s *node = (%s *)object;\n    %s value;\n    while (true) {\n        if (!node->upstream->ops->next((void *)node->upstream, &value)) {\n            return false;\n        }\n        if (node->predicate(value)) {\n            *out = value;\n            return true;\n        }\n    }\n}\n", nodeName, elementSpelling, nodeName, nodeName, elementSpelling)
	fmt.Fprintf(result, "static void %s_destroy(void *object) {\n    %s *node = (%s *)object;\n    hex_stream_free_%s((hex_heap){ node->allocator }, node->upstream);\n}\n", nodeName, nodeName, nodeName, suffix)
	fmt.Fprintf(result, "static const hex_stream_ops_%s %s_ops = { %s_next, %s_destroy };\n", suffix, nodeName, nodeName, nodeName)
	fmt.Fprintf(result, "static inline %s *%s_new(hex_heap h, %s *upstream, bool (*predicate)(%s)) {\n    %s *node = hex_heap_raw_allocate(h.identity, sizeof(%s), _Alignof(%s));\n    node->ops = &%s_ops;\n    node->allocator = h.identity;\n    node->exhausted = false;\n    node->upstream = upstream;\n    node->predicate = predicate;\n    return (%s *)node;\n}\n", stream.CName, nodeName, stream.CName, elementSpelling, nodeName, nodeName, nodeName, nodeName, stream.CName)
}

// writeStreamTakeFamily emits the take adapter node: upstream handle plus a
// remaining count.
func writeStreamTakeFamily(result *strings.Builder, stream compilerTypes.Type) {
	element := stream.Stream.Element
	suffix := streamSuffix(stream)
	elementSpelling := typeSpelling(element)
	nodeName := "hex_stream_take_" + suffix
	fmt.Fprintf(result, "\ntypedef struct %s {\n    const hex_stream_ops_%s *ops;\n    uintptr_t allocator;\n    bool exhausted;\n    %s *upstream;\n    size_t remaining;\n} %s;\n\n", nodeName, suffix, stream.CName, nodeName)
	fmt.Fprintf(result, "static bool %s_next(void *object, %s *out) {\n    %s *node = (%s *)object;\n    if (node->remaining == 0) {\n        return false;\n    }\n    %s value;\n    if (!node->upstream->ops->next((void *)node->upstream, &value)) {\n        return false;\n    }\n    node->remaining--;\n    *out = value;\n    return true;\n}\n", nodeName, elementSpelling, nodeName, nodeName, elementSpelling)
	fmt.Fprintf(result, "static void %s_destroy(void *object) {\n    %s *node = (%s *)object;\n    hex_stream_free_%s((hex_heap){ node->allocator }, node->upstream);\n}\n", nodeName, nodeName, nodeName, suffix)
	fmt.Fprintf(result, "static const hex_stream_ops_%s %s_ops = { %s_next, %s_destroy };\n", suffix, nodeName, nodeName, nodeName)
	fmt.Fprintf(result, "static inline %s *%s_new(hex_heap h, %s *upstream, size_t remaining) {\n    %s *node = hex_heap_raw_allocate(h.identity, sizeof(%s), _Alignof(%s));\n    node->ops = &%s_ops;\n    node->allocator = h.identity;\n    node->exhausted = false;\n    node->upstream = upstream;\n    node->remaining = remaining;\n    return (%s *)node;\n}\n", stream.CName, nodeName, stream.CName, nodeName, nodeName, nodeName, nodeName, stream.CName)
}

// writeStreamMapFamily emits the map adapter node for one concrete
// (source T, mapped U) pair.
func writeStreamMapFamily(result *strings.Builder, spec streamMapSpec) {
	sourceType := spec.sourceType
	streamType := spec.resultType
	element := sourceType.Stream.Element
	mapped := streamType.Stream.Element
	suffix := streamSuffix(streamType)
	elementSpelling := typeSpelling(element)
	mappedSpelling := typeSpelling(mapped)
	nodeName := "hex_stream_map_" + streamSuffix(sourceType) + "_" + suffix
	fmt.Fprintf(result, "\ntypedef struct %s {\n    const hex_stream_ops_%s *ops;\n    uintptr_t allocator;\n    bool exhausted;\n    %s *upstream;\n    %s (*mapper)(%s);\n} %s;\n\n", nodeName, suffix, sourceType.CName, mappedSpelling, elementSpelling, nodeName)
	fmt.Fprintf(result, "static bool %s_next(void *object, %s *out) {\n    %s *node = (%s *)object;\n    %s value;\n    if (!node->upstream->ops->next((void *)node->upstream, &value)) {\n        return false;\n    }\n    *out = node->mapper(value);\n    return true;\n}\n", nodeName, mappedSpelling, nodeName, nodeName, elementSpelling)
	fmt.Fprintf(result, "static void %s_destroy(void *object) {\n    %s *node = (%s *)object;\n    hex_stream_free_%s((hex_heap){ node->allocator }, node->upstream);\n}\n", nodeName, nodeName, nodeName, streamSuffix(sourceType))
	fmt.Fprintf(result, "static const hex_stream_ops_%s %s_ops = { %s_next, %s_destroy };\n", suffix, nodeName, nodeName, nodeName)
	fmt.Fprintf(result, "static inline %s *%s_new(hex_heap h, %s *upstream, %s (*mapper)(%s)) {\n    %s *node = hex_heap_raw_allocate(h.identity, sizeof(%s), _Alignof(%s));\n    node->ops = &%s_ops;\n    node->allocator = h.identity;\n    node->exhausted = false;\n    node->upstream = upstream;\n    node->mapper = mapper;\n    return (%s *)node;\n}\n", streamType.CName, nodeName, sourceType.CName, mappedSpelling, elementSpelling, nodeName, nodeName, nodeName, nodeName, streamType.CName)
}

// writeStreamProduceFamily emits the produce node for one concrete
// (State, T) pair: one header-and-State allocation whose state is a shallow
// copy of the initializer, and a next that invokes the named callback with
// MutPtr<State>.
func writeStreamProduceFamily(result *strings.Builder, spec streamProduceSpec) {
	streamType := spec.streamType
	element := streamType.Stream.Element
	stateType := spec.stateType
	suffix := streamSuffix(streamType)
	stateSpelling := typeSpelling(stateType)
	nodeName := "hex_stream_produce_" + strings.TrimPrefix(stateType.CName, "hex_") + "_" + suffix
	stepUnion := spec.stepUnion
	elementIndex := unionMemberIndex(stepUnion, element)
	eosIndex := unionMemberIndex(stepUnion, compilerTypes.EoS)
	if stepUnion == (compilerTypes.Type{}) || elementIndex < 0 || eosIndex < 0 {
		return
	}
	// The callback is a static function in main.c; the header prototype
	// makes the node's next helper call it before its definition appears.
	fmt.Fprintf(result, "\nstatic %s %s(%s *);\n", stepUnion.CName, spec.callbackName, stateType.CName)
	fmt.Fprintf(result, "typedef struct %s {\n    const hex_stream_ops_%s *ops;\n    uintptr_t allocator;\n    bool exhausted;\n    %s state;\n} %s;\n\n", nodeName, suffix, stateSpelling, nodeName)
	fmt.Fprintf(result, "static bool %s_next(void *object, %s *out) {\n    %s *node = (%s *)object;\n    %s step = %s(&(node->state));\n    if (step.tag == %s) {\n        return false;\n    }\n    *out = step.payload.member_%d;\n    return true;\n}\n", nodeName, typeSpelling(element), nodeName, nodeName, stepUnion.CName, spec.callbackName, unionTagName(stepUnion, eosIndex), elementIndex)
	fmt.Fprintf(result, "static void %s_destroy(void *object) {\n    (void)object;\n}\n", nodeName)
	fmt.Fprintf(result, "static const hex_stream_ops_%s %s_ops = { %s_next, %s_destroy };\n", suffix, nodeName, nodeName, nodeName)
	fmt.Fprintf(result, "static inline %s *%s_new(hex_heap h, %s initial) {\n    %s *node = hex_heap_raw_allocate(h.identity, sizeof(%s), _Alignof(%s));\n    node->ops = &%s_ops;\n    node->allocator = h.identity;\n    node->exhausted = false;\n    node->state = initial;\n    return (%s *)node;\n}\n", streamType.CName, nodeName, stateSpelling, nodeName, nodeName, nodeName, nodeName, streamType.CName)
}

// validateStreamConstructor verifies a checked Stream constructor node.
func validateStreamConstructor(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.OperandType.Stream == nil || !compilerTypes.Equal(node.ResultType, node.OperandType) || node.Element == (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic("stream constructor has invalid checked metadata")
	}
	if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
		return unknownExpressionDiagnostic("stream constructor result does not match its expected type")
	}
	if node.Name == "produce" {
		if len(node.Arguments) != 3 || node.Operand == nil {
			return unknownExpressionDiagnostic("stream produce has invalid checked metadata")
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Name != "new" || len(node.Arguments) != 0 {
		return unknownExpressionDiagnostic("unknown stream constructor")
	}
	return nil
}

// validateStreamMethod verifies a checked Stream or List-source method node.
func validateStreamMethod(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.Element == (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic("stream method has invalid checked metadata")
	}
	for index := range node.Arguments {
		if err := validateCheckedOperandWithState(node.Arguments[index], state); err != nil {
			return err
		}
	}
	switch node.Name {
	case "next":
		if len(node.Arguments) != 0 || node.ResultType.Union == nil {
			return unknownExpressionDiagnostic("stream next has invalid checked metadata")
		}
	case "filter", "take":
		if len(node.Arguments) != 2 || !compilerTypes.Equal(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("stream " + node.Name + " has invalid checked metadata")
		}
	case "map":
		if len(node.Arguments) != 2 || node.ResultType.Stream == nil {
			return unknownExpressionDiagnostic("stream map has invalid checked metadata")
		}
	case "list_stream":
		if len(node.Arguments) != 1 || node.OperandType.List == nil || node.ResultType.Stream == nil {
			return unknownExpressionDiagnostic("list stream has invalid checked metadata")
		}
	case "free":
		if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("stream free has invalid checked metadata")
		}
	default:
		return unknownExpressionDiagnostic("unknown stream method")
	}
	if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
		return unknownExpressionDiagnostic("stream method result does not match its expected type")
	}
	return nil
}

// renderStreamConstructor lowers new() to the canonical empty handle and
// produce() to one header-and-State allocation.
func renderStreamConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Name == "new" {
		return "(&hex_stream_empty_" + streamSuffix(node.ResultType) + ")", nil
	}
	heap, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	initial, err := renderOperandWithState(node.Arguments[1], state)
	if err != nil {
		return "", err
	}
	stateType := node.Arguments[1].Type
	suffix := streamSuffix(node.ResultType)
	nodeName := "hex_stream_produce_" + strings.TrimPrefix(stateType.CName, "hex_") + "_" + suffix
	return fmt.Sprintf("%s_new(%s, %s)", nodeName, heap, initial), nil
}

// renderStreamMethod lowers next, filter, map, take, free, and the List
// source operation.
func renderStreamMethod(node checker.Expression, state *expressionValidation) (string, error) {
	receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
	if receiverErr != nil {
		return "", receiverErr
	}
	switch node.Name {
	case "next":
		return "hex_stream_next_" + streamSuffix(node.OperandType) + "(" + receiver + ")", nil
	case "free":
		heap, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return "hex_stream_free_" + streamSuffix(node.OperandType) + "(" + heap + ", " + receiver + ")", nil
	case "list_stream":
		heap, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		listType := node.OperandType
		return fmt.Sprintf("hex_stream_list_%s_new(%s, %s)", listSuffix(listType), heap, receiver), nil
	case "filter":
		heap, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		callback, err := streamCallbackCName(node.Arguments[1], state.owner)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_stream_filter_%s_new(%s, %s, %s)", streamSuffix(node.ResultType), heap, receiver, callback), nil
	case "take":
		heap, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		count, err := renderOperandWithState(node.Arguments[1], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_stream_take_%s_new(%s, %s, %s)", streamSuffix(node.ResultType), heap, receiver, count), nil
	case "map":
		heap, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		callback, err := streamCallbackCName(node.Arguments[1], state.owner)
		if err != nil {
			return "", err
		}
		nodeName := "hex_stream_map_" + streamSuffix(node.OperandType) + "_" + streamSuffix(node.ResultType)
		return fmt.Sprintf("%s_new(%s, %s, %s)", nodeName, heap, receiver, callback), nil
	default:
		return "", unknownExpressionDiagnostic("unknown stream method")
	}
}

// streamCallbackCName resolves the checked callback operand to the generated
// C function name.
func streamCallbackCName(callback checker.Operand, fallbackOwner string) (string, error) {
	if callback.Node.Kind != checker.FunctionReferenceExpression || callback.Node.Name == "" {
		return "", unknownExpressionDiagnostic("a Stream callback must be a named function")
	}
	return PrivateCName(FunctionName, callback.Node.Name, moduleOwner(callback.Node.Module, fallbackOwner)), nil
}
