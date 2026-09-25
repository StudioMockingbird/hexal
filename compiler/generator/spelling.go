// spelling.go owns shared C declaration spelling: pointer, function, and
// general type spellings shared by every declaration site.
package generator

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// pointerSpelling renders a pointer type's C declarator base from the type
// chain alone, outermost layer contributing pointee `const` exactly when it is
// a read-only Ptr. It never includes the binding's own trailing `const`.
func pointerSpelling(typ compilerTypes.Type) string {
	layers := make([]compilerTypes.Type, 0)
	base := typ
	for base.Element != nil {
		layers = append(layers, base)
		base = *base.Element
	}
	result := base.CName
	for index := len(layers) - 1; index >= 0; index-- {
		if !layers[index].PointeeWritable {
			if index == len(layers)-1 {
				result = "const " + result
			} else {
				result = qualifyLastPointer(result)
			}
		}
		if strings.HasSuffix(result, "*") {
			result += "*"
		} else {
			result += " *"
		}
	}
	return result
}

// declaration builds the complete C declarator for typ bound to name. Every type but Fun<...> is spelled inside the declarator, which is why a CName prefix cannot express it.
func declaration(typ compilerTypes.Type, name string, mutable bool) string {
	if typ.Signature != nil {
		return funDeclaration(typ, name, mutable)
	}
	if compilerTypes.IsString(typ) {
		// A source String value is a pointer-sized handle to an immutable
		// hex_string object; the binding's own const follows the pointer.
		if mutable {
			return "const hex_string *" + name
		}
		return "const hex_string *const " + name
	}
	if compilerTypes.IsList(typ) {
		// A source List value is a pointer-sized owning handle to a mutable
		// heap header; mutation flows through it without a mut binding.
		if mutable {
			return typ.CName + " *" + name
		}
		return typ.CName + " *const " + name
	}
	if compilerTypes.IsDict(typ) {
		if mutable {
			return typ.CName + " *" + name
		}
		return typ.CName + " *const " + name
	}
	if compilerTypes.IsMutex(typ) {
		// A source Mutex value is a pointer-sized handle to a heap-backed
		// control block, exactly like List and Dict; a non-mut binding still
		// calls lock/unlock through it, so only the pointer variable itself
		// gains the top-level const.
		if mutable {
			return typ.CName + " *" + name
		}
		return typ.CName + " *const " + name
	}
	if compilerTypes.IsStash(typ) || compilerTypes.IsPool(typ) {
		// A source Stash or Pool value is a pointer-sized owning handle to
		// heap-backed allocator state, exactly like List, Dict, and Mutex; a
		// non-mut binding still calls allocate/reset/free/destroy through
		// it, so only the pointer variable itself gains the top-level const.
		if mutable {
			return typ.CName + " *" + name
		}
		return typ.CName + " *const " + name
	}
	if typ.Atomic != nil {
		// An Atomic is a mutable-through wrapper; its accessors take a
		// non-const receiver, so the binding carries no top-level const even
		// without a mut declaration.
		return typ.CName + " " + name
	}
	if typ.Element == nil {
		prefix := ""
		if !mutable {
			prefix = "const "
		}
		return prefix + typ.CName + " " + name
	}
	result := pointerSpelling(typ)
	if !mutable {
		result = qualifyLastPointer(result)
	}
	separator := ""
	if strings.HasSuffix(result, "const") {
		separator = " "
	}
	return result + separator + name
}

func qualifyLastPointer(typeName string) string { return typeName + "const" }

// funDeclaration renders a C function-pointer declarator, with name empty when
// the type appears in a position that declares nothing (a parameter of another
// function-pointer type). Its own parameters are always spelled unqualified:
// top-level parameter const lives on the definition's local binding, never on
// the type, and C ignores it when comparing function types.
func funDeclaration(typ compilerTypes.Type, name string, mutable bool) string {
	result := "void"
	if typ.Signature.Result != nil {
		result = typeSpelling(*typ.Signature.Result)
	}
	inner := "*"
	if !mutable {
		inner += "const"
	}
	if name != "" {
		if inner != "*" {
			inner += " "
		}
		inner += name
	}
	parameters := make([]string, len(typ.Signature.Parameters))
	for index, parameter := range typ.Signature.Parameters {
		if typ.Signature.Rest && index == len(typ.Signature.Parameters)-1 {
			// A rest signature's final C parameter is the Slice<T> the ABI
			// passes, not the element type T.
			parameters[index] = typeSpelling(typ.Signature.RestSlice)
			continue
		}
		parameters[index] = typeSpelling(parameter)
	}
	return result + " (" + inner + ")(" + parameterList(parameters) + ")"
}

// standaloneResultSpelling renders typ as a function's own return type. A
// Fun<...> result cannot use its ordinary abstract declarator directly: that
// declarator places the declared name in the middle of itself
// (`RT (*name)(params)`), which cannot nest inside the outer function's own
// `RT name(params)` declarator. C23 typeof turns the abstract declarator into
// a standalone type specifier, the one place typeof is used; every other
// Fun-typed position keeps its existing declarator.
func standaloneResultSpelling(typ compilerTypes.Type) string {
	if typ.Signature != nil {
		return "typeof(" + typeSpelling(typ) + ")"
	}
	return typeSpelling(typ)
}

// typeSpelling renders typ where no name is declared: a function-pointer
// type's parameter and result positions. It never adds a top-level qualifier.
func typeSpelling(typ compilerTypes.Type) string {
	if typ.Signature != nil {
		return funDeclaration(typ, "", true)
	}
	if compilerTypes.IsString(typ) {
		return "const hex_string *"
	}
	if compilerTypes.IsList(typ) {
		return typ.CName + " *"
	}
	if compilerTypes.IsDict(typ) {
		return typ.CName + " *"
	}
	if compilerTypes.IsMutex(typ) {
		return typ.CName + " *"
	}
	if compilerTypes.IsStash(typ) || compilerTypes.IsPool(typ) {
		return typ.CName + " *"
	}
	if typ.Element != nil {
		return pointerSpelling(typ)
	}
	return typ.CName
}
