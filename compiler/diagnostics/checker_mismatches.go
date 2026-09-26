package diagnostics

import "fmt"

type AssignabilityMismatchKind uint8

const (
	OrdinaryInitializerMismatch AssignabilityMismatchKind = iota
	NullableSourceMismatch
	WritablePointerAccessRecovery
	NestedPointerSlotErasure
	PointerErasureRecoveryComposition
)

func InitializerTypeMismatch(target, source string, kind AssignabilityMismatchKind, erased string, hint TextMismatch) Message {
	switch kind {
	case NullableSourceMismatch:
		return message("type.nullable-initializer-mismatch", CategoryType, StageChecker,
			fmt.Sprintf("expected %s; got %s", target, source))
	case WritablePointerAccessRecovery:
		return message("type.pointer-writable-access-recovery", CategoryType, StageChecker,
			source+" cannot recover writable access as "+target)
	case NestedPointerSlotErasure:
		return message("type.pointer-nested-slot-erasure", CategoryType, StageChecker,
			"cannot erase a nested pointer slot as "+target)
	case PointerErasureRecoveryComposition:
		return message("type.pointer-erasure-recovery-composition", CategoryType, StageChecker,
			fmt.Sprintf("expected %s; got %s; erasure and recovery do not compose, bind %s first", target, source, erased))
	default:
		text := fmt.Sprintf("expected %s initializer; got %s", target, source)
		switch hint.Kind {
		case CopyTextToString:
			text += "; use copy(heap)"
		case WidenInlineText:
			text += fmt.Sprintf("; use widen<%d>()", hint.Capacity)
		case ConvertTextFromBytes:
			text += "; use " + hint.Destination + ".from_bytes(...) for a checked conversion"
		}
		return message("type.initializer-type-mismatch", CategoryType, StageChecker, text)
	}
}

func FunctionBindingTypeMismatch(name, expected, actual string) Message {
	return message("type.function-binding-type-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("%s requires %s; got %s", name, expected, actual))
}

func FunctionReturnTypeMismatch(function, expected, actual string, hint TextMismatch) Message {
	text := fmt.Sprintf("%s returns %s; got %s", function, expected, actual)
	switch hint.Kind {
	case CopyTextToString:
		text += "; use copy(heap)"
	case WidenInlineText:
		text += fmt.Sprintf("; use widen<%d>()", hint.Capacity)
	case ConvertTextFromBytes:
		text += "; use " + hint.Destination + ".from_bytes(...) for a checked conversion"
	}
	return message("type.function-return-type-mismatch", CategoryType, StageChecker, text)
}
