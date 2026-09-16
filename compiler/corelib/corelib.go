// Package corelib is the compiler-owned declaration table for core-library
// modules: modules reached only through an `import ... from "std/..."` alias
// that emit no module artifact of their own. It imports only compiler/types,
// never the checker or generator, so the dependency graph stays one-way:
// compiler/types <- compiler/corelib <- compiler/checker, compiler/generator.
package corelib

// Param is one runtime function parameter's shape, at the level corelib can
// express without a checking module's own type Environment: a fixed scalar,
// or a Slice of a fixed element with a fixed writability.
type Param uint8

const (
	ParamHeap Param = iota
	ParamMutByteSlice
)

// Result is one runtime function's success shape. Every Result except
// ResultSize unions with the built-in Error type at the call site.
type Result uint8

const (
	ResultString Result = iota
	ResultStringSlice
	ResultNil
	ResultSize
)

// Function is one exported module function: its parameter shapes, its
// success shape, and the emitted C runtime entry point. A failing Result's
// stable ErrorKind and fixed message are populated by the runtime template
// itself, so they never cross this boundary.
type Function struct {
	Params  []Param
	Result  Result
	Runtime string
}

// Modules is the core-library module table: canonical module path (the
// collection-path import target) to exported function name to declaration.
// A core library emits no module artifact; these are fixed runtime entry
// points selected on demand, never compiler-generated per module.
var Modules = map[string]map[string]Function{
	"std/program": {
		"arguments":             {Result: ResultStringSlice, Runtime: "hex_program_arguments"},
		"current_directory":     {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_current_directory"},
		"home_directory":        {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_home_directory"},
		"temporary_directory":   {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_temporary_directory"},
		"executable_path":       {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_executable_path"},
		"available_parallelism": {Result: ResultSize, Runtime: "hex_program_available_parallelism"},
	},
	"std/entropy": {
		"fill": {Params: []Param{ParamMutByteSlice}, Result: ResultNil, Runtime: "hex_entropy_fill"},
	},
}

// IsModule reports whether path names a known core-library module.
func IsModule(path string) bool {
	_, ok := Modules[path]
	return ok
}

// Lookup resolves one exported function of a known core-library module.
func Lookup(path, name string) (Function, bool) {
	functions, ok := Modules[path]
	if !ok {
		return Function{}, false
	}
	function, ok := functions[name]
	return function, ok
}

// FunctionByRuntime resolves one emitted C runtime entry point back to its
// owning module and declaration. Runtime entry points are unique across every
// core-library function, so a checked call carrying only the runtime name
// still resolves its result shape and parameter list.
func FunctionByRuntime(runtime string) (string, Function, bool) {
	for path, functions := range Modules {
		for _, function := range functions {
			if function.Runtime == runtime {
				return path, function, true
			}
		}
	}
	return "", Function{}, false
}
