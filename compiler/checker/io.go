package checker

// The byte-stream builtins: standard-handle constructors, the memory-stream
// constructor, and the read/write/seek/close operations. Checking reuses the
// existing protected-type machinery end to end -- builtin name dispatch,
// receiver adaptation, freed-state tracking, and structural result unions --
// so no new checked-tree concept exists beyond the three expression kinds.

import (
	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkIOTypeCall resolves IO.stdin(), IO.stdout(), and IO.stderr(). The
// constructors are fallible because the process may lack the requested
// standard handle.
func checkIOTypeCall(call parser.CallExpression, token parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	name := property.Lexeme
	if _, known := compilerTypes.CapabilityFromConstructor(name); !known || len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: token.Name, diagnostic: diagnosticAt(messageAt(token.Name, diagnosticsPkg.UnknownIOOperation()))}
	}
	resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.IOType, compilerTypes.ErrorType})
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: token.Name, diagnostic: diagnosticAt(unknownAt(token.Name))}
	}
	node := Expression{
		Kind:        StreamConstructorExpression,
		Name:        name,
		OperandType: compilerTypes.IOType,
		ResultType:  resultUnion,
		Span:        property.Span,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultUnion, token: property}
}

// checkBytesTypeCall resolves Bytes.over(buffer). The stream borrows the
// caller's List<Byte>; construction records that provenance edge and rejects
// borrowing a list the local facts prove already freed.
func checkBytesTypeCall(call parser.CallExpression, token parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "over" || len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: token.Name, diagnostic: diagnosticAt(messageAt(token.Name, diagnosticsPkg.UnknownBytesOperation()))}
	}
	byteList := ctx.typeEnvironment.ListType(compilerTypes.UInt8)
	if byteList == (compilerTypes.Type{}) {
		return checkedExpression{token: token.Name, diagnostic: diagnosticAt(unknownAt(token.Name))}
	}
	buffer := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(byteList), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(buffer); len(diagnostics) > 0 {
		return buffer
	}
	if !assignable(byteList, buffer.typ) {
		return checkedExpression{token: buffer.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(byteList, buffer.typ, buffer.token))}
	}
	if ctx.names.flow != nil {
		// Construction consults the source list's own release mark; borrow
		// provenance is keyed by stream bindings and does not exist yet here.
		if listID := receiverVariableBinding(buffer.source); listID != 0 && ctx.names.flow.sourceReleased(listID) {
			diagnostic := messageAt(property, diagnosticsPkg.MemoryStreamOutlivesSourceList())
			return checkedExpression{token: property, diagnostic: &diagnostic}
		}
	}
	node := Expression{
		Kind:        BytesOverExpression,
		Arguments:   []Operand{buffer.source},
		OperandType: byteList,
		ResultType:  compilerTypes.BytesType,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.BytesType, Name: "over", Node: node}
	return checkedExpression{source: source, typ: compilerTypes.BytesType, token: property}
}

// checkIOStreamMethodCall resolves read, write, seek, and close on an IO
// value. Capability facts reject provable mismatches before any code is
// generated; unknown capabilities reach the runtime access-mask check.
func checkIOStreamMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	if name != "read" && name != "write" && name != "seek" && name != "close" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownIOMethod(name)))}
	}
	if call.ctx.names.cleanupDepth > 0 && name != "close" {
		// Only close is a cleanup operation; transferring inside a deferred
		// action has no defined evaluation point.
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.OnlyStreamCloseMayBeDeferred()))}
	}
	if diagnostic := streamClosedDiagnostic(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	if name == "read" || name == "write" {
		if diagnostic := streamCapabilityMismatch(name, call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
		}
	}
	var arguments []Operand
	switch name {
	case "read":
		checked, diagnostics := checkStreamReadArguments(call)
		if len(diagnostics) > 0 {
			return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		arguments = checked
	case "write":
		checked, diagnostics := checkStreamWriteArguments(call)
		if len(diagnostics) > 0 {
			return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		arguments = checked
	case "seek":
		checked, diagnostics := checkStreamSeekArguments(call)
		if len(diagnostics) > 0 {
			return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		arguments = checked
	case "close":
		if len(call.call.Arguments) != 0 {
			diagnostic := messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected("close"))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
	}
	resultUnion := streamResultUnion(name, call.ctx.typeEnvironment)
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	node := Expression{
		Kind:        StreamMethodCallExpression,
		Name:        name,
		Operand:     &call.receiver.source.Node,
		Arguments:   arguments,
		OperandType: call.receiver.typ,
		ResultType:  resultUnion,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: name, Node: node}
	checked := checkedExpression{source: source, typ: resultUnion, token: call.callee.Property}
	if name == "close" && call.ctx.names.cleanupDepth == 0 {
		// A deferred close fires only on one exit path, so it proves nothing
		// about later statements and takes the unknown-state envelope.
		markStreamClosed(call.receiver.source, call.callee.Property, call.ctx.names.flow)
	}
	return checked
}

// checkBytesStreamMethodCall resolves read, write, and seek on Bytes. The
// state-changing surface lives on Ptr<mut Bytes>, so the receiver reaches it
// either as a Ptr<mut Bytes> value or as an addressable mutable Bytes binding;
// the shared adaptation rule supplies the address-of and rejects fixed
// bindings.
func checkBytesStreamMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	if name != "read" && name != "write" && name != "seek" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownBytesMethod(name)))}
	}
	if call.ctx.names.cleanupDepth > 0 {
		// Bytes has no cleanup operation; transferring inside a deferred
		// action has no defined evaluation point.
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.MemoryStreamOperationsCannotBeDeferred()))}
	}
	target := call.ctx.typeEnvironment.MutPtrType(compilerTypes.BytesType)
	if target == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	method := MethodDeclaration{Name: name, SelfType: target}
	adapted, diagnostic := adaptReceiver(call.receiver, method, call.callee, call.ctx.typeEnvironment, call.ctx.names.flow)
	if diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	if diagnostic := borrowedSourceDiagnostic(adapted, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	var arguments []Operand
	switch name {
	case "read":
		checked, diagnostics := checkStreamReadArguments(call)
		if len(diagnostics) > 0 {
			return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		arguments = checked
	case "write":
		checked, diagnostics := checkStreamWriteArguments(call)
		if len(diagnostics) > 0 {
			return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		arguments = checked
	case "seek":
		checked, diagnostics := checkStreamSeekArguments(call)
		if len(diagnostics) > 0 {
			return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		arguments = checked
	}
	resultUnion := streamResultUnion(name, call.ctx.typeEnvironment)
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	node := Expression{
		Kind:        StreamMethodCallExpression,
		Name:        name,
		Operand:     &adapted.Node,
		Arguments:   arguments,
		OperandType: adapted.Type,
		ResultType:  resultUnion,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultUnion, token: call.callee.Property}
}

// checkStreamReadArguments checks the destination List<Byte> and the max
// ceiling in written order.
func checkStreamReadArguments(call methodCall) ([]Operand, compilerTypes.Diagnostics) {
	byteList := call.ctx.typeEnvironment.ListType(compilerTypes.UInt8)
	if len(call.call.Arguments) != 2 || byteList == (compilerTypes.Type{}) {
		return nil, compilerTypes.Diagnostics{messageAt(call.callee.Property, diagnosticsPkg.IOReadArity(len(call.call.Arguments)))}
	}
	into := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(byteList), tokenOf(call.call.Arguments[0]), call.ctx)
	if diagnostics := initializerDiagnostics(into); len(diagnostics) > 0 {
		return nil, diagnostics
	}
	if !assignable(byteList, into.typ) {
		return nil, compilerTypes.Diagnostics{typeMismatchDiagnostic(byteList, into.typ, into.token)}
	}
	maximum := checkInitializer(call.call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.call.Arguments[1]), call.ctx)
	if diagnostics := initializerDiagnostics(maximum); len(diagnostics) > 0 {
		return nil, diagnostics
	}
	if !assignable(compilerTypes.SizeType, maximum.typ) {
		return nil, compilerTypes.Diagnostics{typeMismatchDiagnostic(compilerTypes.SizeType, maximum.typ, maximum.token)}
	}
	return []Operand{into.source, maximum.source}, nil
}

// checkStreamWriteArguments checks one read-only Slice<Byte>.
func checkStreamWriteArguments(call methodCall) ([]Operand, compilerTypes.Diagnostics) {
	byteView := call.ctx.typeEnvironment.SliceType(compilerTypes.UInt8, false)
	if len(call.call.Arguments) != 1 || byteView == (compilerTypes.Type{}) {
		return nil, compilerTypes.Diagnostics{messageAt(call.callee.Property, diagnosticsPkg.IOWriteArity(len(call.call.Arguments)))}
	}
	from := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(byteView), tokenOf(call.call.Arguments[0]), call.ctx)
	if diagnostics := initializerDiagnostics(from); len(diagnostics) > 0 {
		return nil, diagnostics
	}
	if !assignable(byteView, from.typ) {
		return nil, compilerTypes.Diagnostics{typeMismatchDiagnostic(byteView, from.typ, from.token)}
	}
	return []Operand{from.source}, nil
}

// checkStreamSeekArguments checks one Seek value.
func checkStreamSeekArguments(call methodCall) ([]Operand, compilerTypes.Diagnostics) {
	if len(call.call.Arguments) != 1 {
		return nil, compilerTypes.Diagnostics{messageAt(call.callee.Property, diagnosticsPkg.IOSeekArity(len(call.call.Arguments)))}
	}
	to := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.SeekType), tokenOf(call.call.Arguments[0]), call.ctx)
	if diagnostics := initializerDiagnostics(to); len(diagnostics) > 0 {
		return nil, diagnostics
	}
	if !assignable(compilerTypes.SeekType, to.typ) {
		return nil, compilerTypes.Diagnostics{typeMismatchDiagnostic(compilerTypes.SeekType, to.typ, to.token)}
	}
	return []Operand{to.source}, nil
}

// streamResultUnion builds the canonical structural result union of one
// operation. Every call site shares one union identity per shape.
func streamResultUnion(name string, typeEnvironment *compilerTypes.Environment) compilerTypes.Type {
	switch name {
	case "read":
		return typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.SizeType, compilerTypes.EoS, compilerTypes.ErrorType})
	case "write", "seek":
		return typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.SizeType, compilerTypes.ErrorType})
	case "close":
		return typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	}
	return compilerTypes.Type{}
}

// streamCapabilityMismatch rejects a read or write the local facts prove the
// binding cannot perform. Unknown capabilities fall through to the runtime
// access-mask check emitted by the generator.
func streamCapabilityMismatch(operation string, receiver Operand, token lexer.Token, state *flowState) *compilerTypes.Diagnostic {
	binding := receiverVariableBinding(receiver)
	if binding == 0 || state == nil {
		return nil
	}
	capability := compilerTypes.StreamCapability(state.capabilityOf(binding))
	switch operation {
	case "read":
		if capability == compilerTypes.StreamWritable {
			diagnostic := messageAt(token, diagnosticsPkg.StreamNotReadable())
			return &diagnostic
		}
	case "write":
		if capability == compilerTypes.StreamReadable {
			diagnostic := messageAt(token, diagnosticsPkg.StreamNotWritable())
			return &diagnostic
		}
	}
	return nil
}

// streamClosedDiagnostic rejects operations through a locally proved closed
// IO binding.
func streamClosedDiagnostic(receiver Operand, token lexer.Token, state *flowState) *compilerTypes.Diagnostic {
	binding := receiverVariableBinding(receiver)
	if binding == 0 || state == nil || !state.freed(binding) {
		return nil
	}
	diagnostic := messageAt(token, diagnosticsPkg.StreamClosedOnEveryPath())
	return &diagnostic
}

// markStreamClosed records the local close fact through the same versioned
// freed machinery Heap.free uses, so rebinding clears it and branch merges
// keep it only when every path closed.
func markStreamClosed(receiver Operand, token lexer.Token, state *flowState) {
	binding := receiverVariableBinding(receiver)
	if binding == 0 || state == nil || !state.tracked[binding] {
		return
	}
	if state.freed(binding) {
		// Unreachable through checkIOStreamMethodCall, which rejects before
		// reaching this point; kept symmetric with the heap-free gate.
		return
	}
	state.markFreed(binding)
}

// borrowedSourceDiagnostic rejects using a Bytes stream whose locally proved
// source list was already freed.
func borrowedSourceDiagnostic(source Operand, token lexer.Token, state *flowState) *compilerTypes.Diagnostic {
	binding := receiverVariableBinding(source)
	if binding == 0 || state == nil {
		return nil
	}
	listID := state.provenance[binding]
	if listID == 0 || !state.releasedSources[listID] {
		return nil
	}
	diagnostic := messageAt(token, diagnosticsPkg.MemoryStreamOutlivesSourceList())
	return &diagnostic
}

// seedStreamBindingFacts seeds what a completed declaration or assignment
// proves about a fresh stream or allocator binding: the IO capability and
// close-tracking registration, the Bytes-to-List borrow edge, or the
// pointer-to-Stash/Pool borrow edge a fresh allocate result records so
// reset/destroy (Stash) or a live-slot check (Pool) can find every locally
// tracked allocation that traces back to one allocator handle.
func seedStreamBindingFacts(flow *flowState, id BindingID, declaredType compilerTypes.Type, source Operand) {
	if flow == nil {
		return
	}
	if compilerTypes.IsIO(declaredType) || compilerTypes.IsFile(declaredType) {
		flow.trackFreed(id)
		flow.setCapability(id, streamInitializerCapability(flow, source.Node))
		return
	}
	if compilerTypes.IsBytes(declaredType) {
		flow.setProvenance(id, bytesSourceBinding(flow, source.Node))
		return
	}
	if declaredType.Element != nil {
		source, kind := allocatorSourceAndKind(source.Node)
		flow.setProvenance(id, source)
		flow.setAllocatorKind(id, kind)
	}
}

// allocatorSourceAndKind classifies a pointer-producing expression: the
// Stash or Pool handle it borrows from, and which allocator kind produced
// it. Zero/unknown means the checker cannot prove a source, which every
// release site accepts.
func allocatorSourceAndKind(node Expression) (BindingID, allocatorKind) {
	switch node.Kind {
	case HeapAllocateExpression, HeapAllocateAlignedExpression:
		return 0, heapAllocator
	case StashMethodCallExpression:
		if node.Name == "allocate" && node.Operand != nil && node.Operand.Kind == VariableExpression {
			return node.Operand.Binding, stashAllocator
		}
	case PoolMethodCallExpression:
		if node.Name == "allocate" && node.Operand != nil && node.Operand.Kind == VariableExpression {
			return node.Operand.Binding, poolAllocator
		}
	}
	return 0, unknownAllocator
}

// streamInitializerCapability reads what an initializing expression proves:
// a constructor carries its own capability, a read of another IO binding
// carries that binding's current fact, everything else is unknown.
func streamInitializerCapability(flow *flowState, node Expression) uint8 {
	inner := &node
	if node.Kind == TryExpression && node.Operand != nil {
		inner = node.Operand
	}
	switch inner.Kind {
	case StreamConstructorExpression:
		if inner.Name == "open" {
			return fileOpenCapability(*inner)
		}
		capability, _ := compilerTypes.CapabilityFromConstructor(inner.Name)
		return uint8(capability)
	case VariableExpression:
		if inner.Binding != 0 {
			return flow.capabilityOf(inner.Binding)
		}
	}
	return uint8(compilerTypes.StreamUnknown)
}

// bytesSourceBinding resolves the List binding a Bytes initializer borrows:
// the constructor's list argument, or the copied binding's recorded source.
func bytesSourceBinding(flow *flowState, node Expression) BindingID {
	if node.Kind == BytesOverExpression && len(node.Arguments) > 0 {
		argument := node.Arguments[0]
		if argument.Node.Kind == VariableExpression && argument.Node.Binding != 0 {
			return argument.Node.Binding
		}
		return 0
	}
	if node.Kind == VariableExpression && node.Binding != 0 {
		return flow.provenance[node.Binding]
	}
	return 0
}

// receiverVariableBinding finds the named binding behind a receiver operand:
// the variable itself, or the variable an inserted address-of wraps.
func receiverVariableBinding(receiver Operand) BindingID {
	node := &receiver.Node
	if node.Kind == AddressOfExpression && node.Operand != nil {
		node = node.Operand
	}
	if node.Kind == VariableExpression {
		return node.Binding
	}
	return 0
}
