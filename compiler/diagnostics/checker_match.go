package diagnostics

func MatchScrutineeNoValue() Message {
	return message("type.match-scrutinee-no-value", CategoryType, StageChecker, "match scrutinee does not produce a value")
}

func MatchArmTypesDiffer() Message {
	return message("type.match-arm-types-differ", CategoryType, StageChecker, "match arm result types do not agree")
}

func MatchElseNotFinal() Message {
	return message("type.match-else-not-final", CategoryType, StageChecker, "else must be the final match arm")
}

func MatchPatternUnreachable() Message {
	return message("type.match-pattern-unreachable", CategoryType, StageChecker, "duplicate or unreachable match pattern")
}

func MatchValuePatternInTypeMode() Message {
	return message("type.match-value-pattern-in-type-mode", CategoryType, StageChecker, "value patterns are not valid in type mode")
}

func MatchTypePatternInValueMode() Message {
	return message("type.match-type-pattern-in-value-mode", CategoryType, StageChecker, "type and variant patterns are not valid in value mode")
}

func MatchPatternWrongType() Message {
	return message("type.match-pattern-wrong-type", CategoryType, StageChecker, "match pattern does not belong to the scrutinee type")
}

func MatchUnsupportedScrutinee(typeName string) Message {
	return message("type.match-unsupported-scrutinee", CategoryType, StageChecker, "match value mode does not support "+typeName+" scrutinees; use Bool, EoS, or an integer-like type")
}

func MatchUnknownQualifiedVariant(owner, name, member string) Message {
	return message("type.match-unknown-qualified-variant", CategoryType, StageChecker, "unknown qualified variant "+owner+"."+name+"."+member)
}
