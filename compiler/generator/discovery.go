// discovery.go owns emission discovery: the module walk that builds one
// moduleEmission and the root-return scan that gates the root entry.
package generator

import (
	"hexal/compiler/checker"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// discoverModuleEmission validates one module and runs every built-in
// discovery walk over it. canonicalID names the module ("graphics/shapes");
// logicalKey is its source-map filename for #line directives
// ("graphics/shapes.hex"). literals is the registry shared by every module
// in dependency-first discovery order.
func discoverModuleEmission(program checker.Program, canonicalID, logicalKey string, literals *literalRegistry, table *span.Table) (*moduleEmission, error) {
	owner := compilerTypes.EncodeModuleOwner(canonicalID)
	functions, functionErr := declaredFunctions(program)
	if functionErr != nil {
		return nil, functionErr
	}
	methods, methodErr := declaredMethods(program)
	if methodErr != nil {
		return nil, methodErr
	}
	emission := &moduleEmission{
		canonicalID: canonicalID,
		logicalKey:  logicalKey,
		program:     program,
		functions:   functions,
		methods:     methods,
	}
	// The literal registry is discovered before the preflight pass: the
	// preflight renders call statements to prove them renderable, and a
	// string-literal argument must resolve against the same registry the
	// emission pass uses.
	emission.stringState = literals
	emission.stringUsed = discoverGeneratedStrings(program, literals)
	emission.interpolationUsed = discoverInterpolationUsed(program)
	if validationErr := validateCheckedProgram(program, functions, methods, literals, table); validationErr != nil {
		return nil, validationErr
	}
	emission.errorUsed = discoverErrorUsed(program)
	objects, objectErr := objectDefinitions(program)
	if objectErr != nil {
		return nil, objectErr
	}
	emission.objects = objects
	unionState, unionErr := discoverGeneratedUnions(program)
	if unionErr != nil {
		return nil, unionErr
	}
	emission.unionState = unionState
	heapState, heapErr := discoverHeapHelpers(program)
	if heapErr != nil {
		return nil, heapErr
	}
	emission.heapState = heapState
	stashState, stashErr := discoverStashHelpers(program)
	if stashErr != nil {
		return nil, stashErr
	}
	emission.stashState = stashState
	emission.poolState = discoverGeneratedPool(program)
	emission.adtState = discoverGeneratedADTs(program)
	arrayState := discoverGeneratedArrays(program)
	emission.arrayState = arrayState
	sliceState := discoverGeneratedSlices(program)
	emission.sliceState = sliceState
	listState := discoverGeneratedLists(program)
	emission.listState = listState
	dictState := discoverGeneratedDicts(program)
	emission.dictState = dictState
	emission.equalityState = discoverEqualityTypes(program)
	conversionSpecs, sizeLiterals := discoverGeneratedConversions(program)
	emission.conversionSpecs = conversionSpecs
	emission.sizeLiterals = sizeLiterals
	emission.divisionTypes = discoverGeneratedDivisions(program)
	emission.shiftSpecs = discoverGeneratedShifts(program)
	emission.bitCastSpecs = discoverGeneratedBitCasts(program)
	emission.endianSpecs = discoverGeneratedEndian(program)
	printState, printErr := discoverGeneratedPrint(program)
	if printErr != nil {
		return nil, printErr
	}
	emission.printState = printState
	emission.ioState = discoverGeneratedStreams(program, logicalKey, literals)
	emission.timeState = discoverGeneratedTime(program, logicalKey, literals)
	emission.fileState = discoverGeneratedFiles(program, logicalKey, literals)
	emission.textState = discoverGeneratedText(program, logicalKey, literals)
	emission.networkState = discoverGeneratedNetwork(program, logicalKey, literals)
	emission.processState = discoverGeneratedProcess(program, logicalKey, literals)
	emission.signalState = discoverGeneratedSignal(program, logicalKey, literals)
	emission.terminalState = discoverGeneratedTerminal(program, logicalKey, literals)
	emission.corelibState = discoverGeneratedCorelib(program, logicalKey, literals)
	emission.rootReturn = discoverGeneratedRootReturn(program)
	emission.wrapState = discoverGeneratedWraps(program)
	concurrencyState, concurrencyErr := discoverGeneratedConcurrency(program, functions, literals, canonicalID, owner, logicalKey)
	if concurrencyErr != nil {
		return nil, concurrencyErr
	}
	emission.concurrencyState = concurrencyState
	if concurrencyState.used {
		// The task runtime needs the String typedefs, the inline text structs, and the
		// Error object for the failure Errors every recoverable operation
		// constructs; the discovery pass registered the literals. The
		// Channel and Mutex helpers take a hex_heap argument, so the heap
		// machinery is required too.
		literals.used = true
		literals.requireErrorText()
		emission.stringUsed = true
		heapState.required = true
	}
	if len(emission.timeState.wallUnions) > 0 {
		// WallTime.now builds its failure Error from the module file literal
		// and a static String message.
		literals.used = true
		literals.requireErrorText()
		emission.stringUsed = true
		emission.errorUsed = true
	}
	if emission.errorUsed {
		// Error's representation names the String handle and the inline header and
		// message types, so the
		// string component is a required dependency of error.h.
		literals.used = true
		literals.requireErrorText()
		emission.stringUsed = true
	}
	if emission.corelibState != nil && emission.corelibState.used {
		// A core-library call's result union names Error and String, and the
		// program/entropy component headers include the Heap and Slice
		// definitions those result structs reference.
		literals.used = true
		literals.requireErrorText()
		emission.stringUsed = true
		emission.errorUsed = true
		heapState.required = true
		sliceState.required = true
	}
	if len(dictState.order) > 0 {
		// The dict component header declares its String dependency, so the
		// string component must exist.
		literals.used = true
		emission.stringUsed = true
	}
	if (emission.ioState != nil && emission.ioState.used) || (emission.printState != nil && emission.printState.used) || emission.fileState.used ||
		(emission.networkState != nil && emission.networkState.tcp) || (emission.processState != nil && emission.processState.operations) {
		// print's descriptor write-all sink selects hexal/io.c exactly like a
		// direct stream operation does (see io_component.go's own selection
		// condition), so it carries the identical dependency set: the Byte
		// list, the byte Slice, the Error object with its String and inline text
		// fields, heap allocation through List growth, and the shared trap.
		// tcp_read and pipe_read reference hex_list_UInt8 and its
		// reserve/grow helpers unconditionally in their generated C, so a
		// program reaching either without ever writing List<Byte> itself
		// still needs it forced reachable, exactly like File's own read.
		ensureByteList(listState)
		ensureSliceUInt8(sliceState)
		literals.used = true
		literals.requireErrorText()
		emission.stringUsed = true
		emission.errorUsed = true
		sliceState.required = true
		heapState.required = true
	}
	if emission.stringUsed {
		ensureSliceUInt8(sliceState)
		// The String helpers allocate through the heap machinery, and the
		// string component header declares its Heap and Slice dependencies.
		heapState.required = true
		sliceState.required = true
	}
	if len(listState.order) > 0 || len(dictState.order) > 0 {
		// The List and Dict helpers allocate and trap through the heap
		// machinery and the shared runtime trap.
		heapState.required = true
	}
	if stashState.required || len(emission.poolState.order) > 0 {
		// Both allocator cores back every block/slot allocation through
		// hex_heap_allocate/hex_heap_free directly.
		heapState.required = true
	}
	if collectionsNeedSlice(arrayState, listState, sliceState) {
		// Only a slice helper names the slice component, and the templates
		// guard those on the same fact. Selecting Slice for every program
		// that merely has an array would emit a component holding nothing
		// but its include guard.
		sliceState.required = true
	}
	if emission.errorUsed {
		// Selecting Error registers every ErrorKind variant into the
		// program-wide tag registry, even when no source expression in this
		// module constructs one: hex_error_kind_header's switch must always
		// be complete.
		emission.adtState.ensureRegistered(compilerTypes.ErrorKindType)
	}
	if emission.textState != nil && emission.textState.runeCategory {
		// Selecting Rune.category registers every UnicodeCategory variant into
		// the program-wide tag registry: the module's category-tag table must
		// cover all thirty even when no source expression names one.
		emission.adtState.ensureRegistered(compilerTypes.UnicodeCategoryType)
	}
	if emission.textState != nil && len(emission.textState.normalize) > 0 {
		// Selecting String.normalize registers every NormalizationForm variant
		// into the program-wide tag registry: the module's form map must cover
		// all four even when no source expression names one.
		emission.adtState.ensureRegistered(compilerTypes.NormalizationFormType)
	}
	emission.typeState = &generatedTypeValidation{declaredObjects: errorDeclaredObjects(program), arrays: arrayState}
	return emission, nil
}

// discoverGeneratedRootReturn reports whether the module's root scope
// contains a checked root return. The checker admits one only at entry-module
// scope, so any occurrence selects the entry status slot and cleanup label.
func discoverGeneratedRootReturn(program checker.Program) bool {
	found := false
	visitor := &programVisitor{
		Statement: func(statement checker.Statement) error {
			if _, ok := statement.(checker.RootReturnStatement); ok {
				found = true
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return false
	}
	return found
}
