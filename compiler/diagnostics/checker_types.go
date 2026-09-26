package diagnostics

import "fmt"

func UnknownType(name string) Message {
	text := "unknown type " + name
	if name == "Strand" {
		text += "; use String<N> (String<31> keeps the former capacity)"
	}
	return message("type.unknown-type", CategoryType, StageChecker, text)
}

func NilOutsideUnion() Message {
	return message("type.nil-outside-union", CategoryType, StageChecker, "Nil is valid only as a member of a union with a non-Nil type")
}

func NumericLiteralOnlyStringCapacity() Message {
	return message("type.numeric-literal-only-string-capacity", CategoryType, StageChecker, "a numeric literal is only valid as a String capacity")
}

func InvalidStringCapacityLiteral() Message {
	return message("type.string-capacity-literal-invalid", CategoryType, StageChecker, "String capacity must be a positive integer literal")
}

func StringCapacityExceedsMaximum(capacity, maximum uint64) Message {
	return message("type.string-capacity-exceeds-maximum", CategoryType, StageChecker,
		fmt.Sprintf("String capacity %d exceeds the maximum of %d", capacity, maximum))
}

func StringCapacityArgumentCount() Message {
	return message("type.string-capacity-argument-count", CategoryType, StageChecker, "String takes at most one capacity argument")
}

func UnsupportedTypeSpelling(spelling string) Message {
	return message("type.unsupported-type-spelling", CategoryType, StageChecker, spelling+" is not supported")
}

func MutTypeArgumentPosition() Message {
	return message("type.mut-type-argument-position", CategoryType, StageChecker, "mut is only allowed immediately inside Ptr<...> or Slice<...>")
}

func DuplicateUnionMember(name string) Message {
	return message("type.duplicate-union-member", CategoryType, StageChecker, "union member "+name+" appears more than once")
}

func UnknownNilUnion() Message {
	return message("type.unknown-nil-union", CategoryType, StageChecker, "Unknown | Nil is not a value type; use Ptr<Unknown> | Nil")
}

func InvalidUnionType() Message {
	return message("type.invalid-union", CategoryType, StageChecker, "could not construct union type")
}

func InvalidPointerType() Message {
	return message("type.invalid-pointer-type", CategoryType, StageChecker, "could not construct pointer type")
}

func UnknownNeedsPointer() Message {
	return message("type.unknown-needs-pointer", CategoryType, StageChecker, "Unknown has no known size or layout; it may only be used behind a pointer")
}

func FunctionResultNotStorable(name string) Message {
	return message("type.function-result-not-storable", CategoryType, StageChecker, "function result "+name+" is not storable")
}

func InvalidRestElementType(name string) Message {
	return message("type.invalid-rest-element", CategoryType, StageChecker, name+" is not a valid rest element type")
}

func UnknownModuleAlias(name string) Message {
	return message("module.unknown-alias", CategoryModule, StageChecker, "unknown module alias "+name)
}

func ImportedModuleHasExecutableStatements(module string) Message {
	return message("module.imported-executable-statements", CategoryModule, StageChecker, "imported module "+module+" contains executable statements")
}

func ImportedModuleMutableBinding(module, name string) Message {
	return message("module.imported-mutable-binding", CategoryModule, StageChecker, "imported module "+module+" cannot declare mutable top-level binding "+name+"; pass explicit state instead")
}
