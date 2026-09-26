package diagnostics

import "fmt"

func ConflictingForeignCSymbol(name string) Message {
	return message("type.conflicting-foreign-c-symbol", CategoryType, StageChecker,
		"conflicting foreign declarations for C symbol "+name)
}

func InvalidForeignCSpelling(name string) Message {
	return message("type.invalid-foreign-c-spelling", CategoryType, StageChecker, "invalid C spelling "+name)
}

func UnsupportedForeignABIMapping(name, target string) Message {
	return message("type.unsupported-foreign-abi-mapping", CategoryType, StageChecker,
		name+" has no supported C ABI mapping for target "+target)
}

func ForeignNameAlreadyDeclared(name string) Message {
	return message("type.foreign-name-already-declared", CategoryType, StageChecker, name+" is already declared")
}

func ForeignMemberAlreadyDeclared(name string) Message {
	return message("type.foreign-member-already-declared", CategoryType, StageChecker,
		"member "+name+" is declared more than once")
}

func ForeignCSpellingCannotBeProven(spelling string) Message {
	return message("type.foreign-c-spelling-unproven", CategoryType, StageChecker,
		"C spelling "+spelling+" cannot be proven in a handwritten binding; use an automatic C import or expose a C wrapper")
}

func ForeignTypeIncomplete(name, position string) Message {
	return message("type.foreign-type-incomplete", CategoryType, StageChecker,
		fmt.Sprintf("foreign type %s is incomplete in %s", name, position))
}

func ForeignTypeIncompleteValue(name string) Message {
	return message("type.foreign-type-incomplete-value", CategoryType, StageChecker,
		"foreign type "+name+" is incomplete in a value position")
}
