package diagnostics

import "fmt"

func InlineStringCapacityArity() Message {
	return message("type.inline-string-capacity-arity", CategoryType, StageChecker, "String takes at most one capacity argument")
}
func UnknownStringConstructor() Message {
	return message("type.unknown-string-constructor", CategoryType, StageChecker, "String has no such operation; use String.from_bytes(heap, view), String.from_runes(heap, runes), or String.interpolate(heap, template)")
}
func TextResultUnionUnavailable(name string) Message {
	return message("type.text-result-union-unavailable", CategoryType, StageChecker, name+" has no result union with Error")
}
func ByteViewRequired(operation, actual string) Message {
	return message("type.byte-view-required", CategoryType, StageChecker, operation+" requires Slice<Byte>; got "+actual)
}
func StringConstructorRequiresHeap(operation, actual string) Message {
	return message("type.string-constructor-requires-heap", CategoryType, StageChecker, operation+" requires a Heap; got "+actual)
}
func StringFromRunesRequiresRuneSlice(actual string) Message {
	return message("type.string-from-runes-requires-rune-slice", CategoryType, StageChecker, "String.from_runes requires Slice<Rune>; got "+actual)
}
func InlineStringConstructorArity(operation string, want, got int) Message {
	return message("type.inline-string-constructor-arity", CategoryType, StageChecker, fmt.Sprintf("%s expects %d arguments; got %d", operation, want, got))
}
func InlineInterpolationConstructorArity(operation string, got int) Message {
	return message("type.inline-interpolation-arity", CategoryType, StageChecker, fmt.Sprintf("%s expects 1 argument; got %d", operation, got))
}
func UnknownInlineStringOperation(name string) Message {
	return message("type.unknown-inline-string-operation", CategoryType, StageChecker, name+" has no such operation; use "+name+".from_bytes(view), "+name+".concat(left, right), or "+name+".interpolate(template)")
}
func InterpolationNeedsEmbeddedValue(operation string) Message {
	return message("type.interpolation-needs-value", CategoryType, StageChecker, operation+" requires at least one interpolation")
}
func InterpolationRequiresInterpretedTemplate(operation string) Message {
	return message("type.interpolation-requires-interpreted-template", CategoryType, StageChecker, operation+" requires an interpreted interpolation template")
}
func InterpolationTypeUnsupported(name string) Message {
	return message("type.interpolation-type-unsupported", CategoryType, StageChecker, "string interpolation does not support "+name)
}
func StringInterpolateArity(got int) Message {
	return message("type.string-interpolate-arity", CategoryType, StageChecker, fmt.Sprintf("String.interpolate expects 2 arguments; got %d", got))
}
func InlineTextLiteralCapacity(name string, capacity uint64) Message {
	return message("type.inline-text-literal-capacity", CategoryType, StageChecker, fmt.Sprintf("%s literal exceeds %d UTF-8 bytes", name, capacity))
}
func TextValueRequired(label, actual string) Message {
	return message("type.text-value-required", CategoryType, StageChecker, label+" requires text; got "+actual)
}
func UnknownTextMethod(receiver, name string) Message {
	return message("type.unknown-text-method", CategoryType, StageChecker, receiver+" has no method "+name)
}
func TextMethodNoArguments(name string) Message {
	return message("type.text-method-no-arguments", CategoryType, StageChecker, name+" expects no arguments")
}
func TemporaryCannotRootTextView(kind, receiver string) Message {
	return message("type.temporary-cannot-root-text-view", CategoryType, StageChecker, "a "+kind+" cannot be rooted in a temporary "+receiver)
}
func TextMethodArity(name string, expected, got int) Message {
	unit := "arguments"
	if expected == 1 {
		unit = "argument"
	}
	return message("type.text-method-arity", CategoryType, StageChecker, fmt.Sprintf("%s expects %d %s; got %d", name, expected, unit, got))
}
func StringMethodHeapRequired(name, actual string) Message {
	return message("type.string-method-heap-required", CategoryType, StageChecker, name+" requires a Heap; got "+actual)
}
func NormalizeRequiresForm(actual string) Message {
	return message("type.normalize-form-required", CategoryType, StageChecker, "normalize requires a NormalizationForm; got "+actual)
}
func WidenRequiresInlineString(receiver string) Message {
	return message("type.widen-requires-inline-string", CategoryType, StageChecker, receiver+" has no method widen")
}
func WidenRequiresCapacityArgument() Message {
	return message("type.widen-requires-capacity", CategoryType, StageChecker, "widen requires exactly one capacity argument")
}
func WidenCapacityMustNotShrink() Message {
	return message("type.widen-capacity-must-not-shrink", CategoryType, StageChecker, "widen<M> requires M greater than or equal to N")
}
func CannotFreeStringLiteral() Message {
	return message("type.cannot-free-string-literal", CategoryType, StageChecker, "cannot free a String literal")
}
func CStringPointerRequiresHeapString(receiver string) Message {
	return message("type.c-string-pointer-requires-heap-string", CategoryType, StageChecker, receiver+" has no method c_pointer")
}
func CStringPointerResultUnavailable() Message {
	return message("type.c-string-pointer-result-unavailable", CategoryType, StageChecker, "String.c_pointer has no pointer result")
}
