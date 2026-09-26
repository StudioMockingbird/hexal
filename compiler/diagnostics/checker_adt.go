package diagnostics

import "fmt"

func ADTNeedsVariants() Message {
	return message("type.adt-needs-variants", CategoryType, StageChecker, "ADT declarations require at least two variants")
}

func DuplicateADTVariant() Message {
	return message("type.duplicate-adt-variant", CategoryType, StageChecker, "ADT variant name is duplicated")
}

func DuplicateVariantPayloadField(name string) Message {
	return message("type.duplicate-variant-payload-field", CategoryType, StageChecker,
		"variant payload declares field "+name+" more than once")
}

func ADTRecursionHasNoFiniteRepresentation() Message {
	return message("type.adt-recursion-no-finite-representation", CategoryType, StageChecker, "ADT recursion has no finite representation")
}

func AtomicPayloadCannotBeCopied() Message {
	return message("type.atomic-payload-cannot-be-copied", CategoryType, StageChecker,
		"Atomic values cannot be copied, assigned, addressed, or stored here")
}

func UnsupportedADTPayloadType(name string) Message {
	return message("type.unsupported-adt-payload-type", CategoryType, StageChecker, "unsupported ADT payload field type "+name)
}

func CannotInferFor(owner string) Message {
	return message("type.cannot-infer-for", CategoryType, StageChecker,
		fmt.Sprintf("cannot infer generic parameter for %s", owner))
}

func UnknownQualifiedVariant(owner, variant string) Message {
	return message("type.unknown-qualified-variant", CategoryType, StageChecker,
		fmt.Sprintf("unknown qualified variant %s.%s", owner, variant))
}

func UnknownVariant(owner, variant string) Message {
	return message("type.unknown-variant", CategoryType, StageChecker,
		fmt.Sprintf("unknown variant %s.%s", owner, variant))
}

func VariantTakesNoArguments(owner, variant string) Message {
	return message("type.variant-takes-no-arguments", CategoryType, StageChecker,
		fmt.Sprintf("%s.%s takes no arguments", owner, variant))
}

func VariantRequiresPayload(owner, variant string) Message {
	return message("type.variant-requires-payload", CategoryType, StageChecker,
		fmt.Sprintf("%s.%s requires a payload", owner, variant))
}

func ConstructorArgumentsMustBeNamed() Message {
	return message("type.constructor-arguments-must-be-named", CategoryType, StageChecker, "constructor arguments must be named")
}

func VariantHasNoField(variant, field string) Message {
	return message("type.variant-has-no-field", CategoryType, StageChecker,
		fmt.Sprintf("%s has no field named %s", variant, field))
}

func DuplicateVariantField(variant, field string) Message {
	return message("type.duplicate-variant-field", CategoryType, StageChecker,
		fmt.Sprintf("%s initializes field %s more than once", variant, field))
}

func MissingVariantPayloadField(name string) Message {
	return message("type.missing-variant-payload-field", CategoryType, StageChecker,
		"variant constructor requires the payload field "+name)
}

func ADTPayloadFieldsRequireNarrowing() Message {
	return message("type.adt-payload-requires-narrowing", CategoryType, StageChecker,
		"ADT payload fields are only accessible inside a narrowed match arm")
}

func ErrorKindPayloadRequiresHeader() Message {
	return message("type.error-kind-payload-requires-header", CategoryType, StageChecker,
		"ErrorKind payload is available only through header()")
}

func TypeHasNoField(typeName, field string) Message {
	return message("type.type-has-no-field", CategoryType, StageChecker,
		fmt.Sprintf("%s has no field named %s", typeName, field))
}

func MatchRequiresFinalElse(typeName string) Message {
	return message("type.match-requires-final-else", CategoryType, StageChecker,
		fmt.Sprintf("match on %s requires a final else", typeName))
}

func ErrorKindMatchRequiresFinalElse() Message {
	return message("type.error-kind-match-requires-final-else", CategoryType, StageChecker,
		"match on ErrorKind requires a final else arm")
}

func MatchNotExhaustive(missing string) Message {
	return message("type.match-not-exhaustive", CategoryType, StageChecker,
		"match is not exhaustive; missing "+missing)
}
