package diagnostics

import "fmt"

func ValueNameConflictsWithType(name string) Message {
	return message("type.value-name-conflicts-with-type", CategoryType, StageChecker,
		"value "+name+" is already declared as a type")
}

func DuplicateParameter(name string) Message {
	return message("type.duplicate-parameter", CategoryType, StageChecker,
		"parameter "+name+" is declared more than once")
}

func FunctionParameterNotShallowCopyable(name string) Message {
	return message("type.function-parameter-not-shallow-copyable", CategoryType, StageChecker,
		"function parameter "+name+" is not shallow-copyable")
}

func FunctionResultNotShallowCopyable(name string) Message {
	return message("type.function-result-not-shallow-copyable", CategoryType, StageChecker,
		"function result "+name+" is not shallow-copyable")
}

func FunctionMayFallThrough(function, result string) Message {
	return message("type.function-may-fall-through", CategoryType, StageChecker,
		fmt.Sprintf("returning %s may fall through without returning %s", function, result))
}

func BuiltinTypeCannotBeRedeclared(name string, constructor bool) Message {
	if constructor {
		return message("type.builtin-type-constructor-redeclared", CategoryType, StageChecker,
			"built-in type constructor "+name+" cannot be redeclared")
	}
	return message("type.builtin-type-redeclared", CategoryType, StageChecker,
		"built-in type "+name+" cannot be redeclared")
}

func TypeAlreadyDeclared(name string) Message {
	return message("type.already-declared", CategoryType, StageChecker, "type "+name+" is already declared")
}

func TypeAlreadyDeclaredAsValue(name string) Message {
	return message("type.already-declared-as-value", CategoryType, StageChecker,
		"type "+name+" is already declared as a value")
}

func TypeAliasCannotReferenceItself(name string) Message {
	return message("type.alias-self-reference", CategoryType, StageChecker,
		"type alias "+name+" cannot reference itself")
}

func DuplicateObjectMember(object, member string) Message {
	return message("type.duplicate-object-member", CategoryType, StageChecker,
		fmt.Sprintf("object type %s declares member %s more than once", object, member))
}

func ObjectCannotContainItselfByValue(name string) Message {
	return message("type.object-contains-itself-by-value", CategoryType, StageChecker,
		"object type "+name+" cannot contain itself by value")
}

func UnsupportedObjectMemberType(name string) Message {
	return message("type.unsupported-object-member-type", CategoryType, StageChecker,
		"unsupported object member type "+name)
}

func VariableAlreadyDeclaredInScope(name string) Message {
	return message("type.variable-already-declared-in-scope", CategoryType, StageChecker,
		"variable "+name+" is already declared in this scope; use '=' for reassignment")
}

func ContextualInitializerNeedsAnnotation() Message {
	return message("type.contextual-initializer-needs-annotation", CategoryType, StageChecker,
		"`let` requires an initializer whose type does not depend on context; annotate the binding instead")
}

func CannotAssignToSelf() Message {
	return message("type.cannot-assign-to-self", CategoryType, StageChecker, "cannot assign to self; self is a fixed binding")
}

func CannotAssignToFunction(name string) Message {
	return message("type.cannot-assign-to-function", CategoryType, StageChecker, "cannot assign to function "+name)
}

func CannotAssignToParameter(name string) Message {
	return message("type.cannot-assign-to-parameter", CategoryType, StageChecker,
		"cannot assign to parameter "+name+"; parameters are fixed bindings")
}

func LoopBinderIsImmutable(name string) Message {
	return message("type.loop-binder-immutable", CategoryType, StageChecker, "loop binder "+name+" is immutable")
}

func CannotAssignToConstant(name string) Message {
	return message("type.cannot-assign-to-constant", CategoryType, StageChecker, "cannot assign to constant "+name)
}

func CannotAssignToReadOnlyMember(place string) Message {
	return message("type.cannot-assign-to-read-only-member", CategoryType, StageChecker,
		"cannot assign to read-only member "+place)
}

func CannotWriteThroughReadOnlyPointer(place string) Message {
	return message("type.cannot-write-through-read-only-pointer", CategoryType, StageChecker,
		"cannot write through a read-only pointer "+place)
}

func FunctionCannotAccessModuleBinding(owner, binding string) Message {
	return message("type.function-cannot-access-module-binding", CategoryType, StageChecker,
		fmt.Sprintf("function %s cannot access module data binding %s; pass it as a parameter", owner, binding))
}

func SelfNotBoundOutsideMethod() Message {
	return message("type.self-not-bound-outside-method", CategoryType, StageChecker,
		"self is not bound outside a method body")
}
