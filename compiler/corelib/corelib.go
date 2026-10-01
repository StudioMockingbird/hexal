// Package corelib is the adapter over the compiler-owned core-library registry:
// modules reached only through an `import ... from std.<module>` alias that emit
// no module artifact of their own. The declarations live in compiler/specdata as
// identifier-bearing records; this package resolves those identifiers to
// compiler/types identities and exposes them to the checker and generator. It
// imports compiler/specdata and compiler/types, never the checker or generator,
// so the dependency graph stays one-way: compiler/specdata, compiler/types <-
// compiler/corelib <- compiler/checker, compiler/generator.
package corelib

import (
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// Param is one runtime function parameter's shape, at the level a core-library
// declaration can express without a checking module's own type Environment: a
// fixed scalar, or a Slice of a fixed element with a fixed writability. The
// declaration lives in compiler/specdata.
type Param = specdata.CoreParam

const (
	ParamHeap         Param = specdata.CoreParamHeap
	ParamMutByteSlice Param = specdata.CoreParamMutByteSlice
	ParamString       Param = specdata.CoreParamString
	ParamValue        Param = specdata.CoreParamValue
	ParamPattern      Param = specdata.CoreParamPattern
	ParamSpan         Param = specdata.CoreParamSpan
	ParamMatch        Param = specdata.CoreParamMatch
)

// Result is one runtime function's success shape. Every Result except
// ResultSize unions with the built-in Error type at the call site. The
// declaration lives in compiler/specdata.
type Result = specdata.CoreResult

const (
	ResultString      Result = specdata.CoreResultString
	ResultStringSlice Result = specdata.CoreResultStringSlice
	ResultNil         Result = specdata.CoreResultNil
	ResultSize        Result = specdata.CoreResultSize
	ResultValue       Result = specdata.CoreResultValue
	ResultNoValue     Result = specdata.CoreResultNoValue
	ResultPattern     Result = specdata.CoreResultPattern
	ResultBool        Result = specdata.CoreResultBool
	ResultSpanNil     Result = specdata.CoreResultSpanNil
	ResultMatchNil    Result = specdata.CoreResultMatchNil
)

// Function is one exported module function, the registry record a lookup
// returns. The declaration lives in compiler/specdata.
type Function = specdata.CoreFunction

// Method is one core-library instance method, the registry record a lookup
// returns. The declaration lives in compiler/specdata.
type Method = specdata.CoreMethod

// IsModule reports whether path names a known core-library module.
func IsModule(path string) bool {
	return specdata.CoreModuleKnown(specdata.CoreModuleID(path))
}

// LookupType resolves one exported type of a known core-library module to its
// canonical compiler/types identity. The registry holds the export name and a
// type identifier; resolveCoreType is the adapter that crosses back to the
// live Type.
func LookupType(path, name string) (compilerTypes.Type, bool) {
	module, ok := specdata.CoreModuleByID(specdata.CoreModuleID(path))
	if !ok {
		return compilerTypes.Type{}, false
	}
	for _, export := range module.Types {
		if export.Name == name {
			return resolveCoreType(export.TypeID)
		}
	}
	return compilerTypes.Type{}, false
}

// Lookup resolves one exported function of a known core-library module.
func Lookup(path, name string) (Function, bool) {
	return specdata.CoreFunctionByModuleName(specdata.CoreModuleID(path), name)
}

// LookupMethod resolves name on a receiver of exactly type receiver. The
// registry keys a method by receiver identifier, so each candidate's receiver
// resolves to its canonical Type before the comparison.
func LookupMethod(receiver compilerTypes.Type, name string) (Method, bool) {
	for _, method := range specdata.CoreMethodsNamed(name) {
		if typ, ok := resolveCoreType(method.Receiver); ok && compilerTypes.Equal(typ, receiver) {
			return method, true
		}
	}
	return Method{}, false
}

// FunctionByRuntime resolves one emitted C runtime entry point back to its
// owning module and declaration. Runtime entry points are unique across every
// core-library function, so a checked call carrying only the runtime name
// still resolves its result shape and parameter list.
func FunctionByRuntime(runtime string) (string, Function, bool) {
	module, function, ok := specdata.CoreFunctionByRuntime(runtime)
	return string(module), function, ok
}

// resolveCoreType maps one registry type identifier to its canonical
// compiler/types identity through the registry's shared adapter. The registry
// stores identifiers because it imports no compiler package; ResolveSpecID is
// the one place that crosses back to the live Type.
func resolveCoreType(id specdata.CoreTypeID) (compilerTypes.Type, bool) {
	return compilerTypes.ResolveSpecID(specdata.TypeID(id))
}
