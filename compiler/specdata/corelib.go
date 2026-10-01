package specdata

import "fmt"

// Core-library contracts: the modules reached through an `import ... from
// std.<module>` alias that publish no Hexal source and emit no module artifact
// of their own. A record names a module's exported types and functions.
//
// A type export carries an identifier, never a Type. This package imports no
// compiler package, and compiler/corelib already imports compiler/types and
// stores live Type values, so a Type here would close a cycle. An adapter on
// the corelib side resolves the identifier to its canonical identity; the
// registry stays the single owner of which names a module exports.

// CoreModuleID is one core-library module's canonical import path, the
// collection-path spelling (`std/io`) rather than the dotted alias a program
// writes.
type CoreModuleID string

// CoreTypeID identifies one canonical compiler-owned type a module exports. It
// is an identifier, never a Type: the value crosses the package boundary in
// place of the Type value. One canonical type has one identifier, which may be
// exported under one name by several modules.
type CoreTypeID string

// CoreParam is one function parameter's runtime shape, the only level a
// core-library declaration expresses without a checking module's type
// Environment.
type CoreParam uint8

const (
	CoreParamHeap CoreParam = iota
	CoreParamMutByteSlice
	// CoreParamString is a validated heap String parameter.
	CoreParamString
	// CoreParamValue is a Json.Value parameter.
	CoreParamValue
	// CoreParamPattern is an owning compiled-regex handle parameter.
	CoreParamPattern
	// CoreParamSpan is a half-open byte-range value parameter.
	CoreParamSpan
	// CoreParamMatch is an owning Match value parameter.
	CoreParamMatch
	// CoreParamUInt16 is a port number.
	CoreParamUInt16
	// CoreParamBytes is a read-only Slice<Byte> parameter.
	CoreParamBytes
	// CoreParamConfig is a ServerConfig record value parameter.
	CoreParamConfig
	// CoreParamRouter is a Router<App> parameter; App comes from the call's
	// type argument or its receiver.
	CoreParamRouter
	// CoreParamServer is a Server<App> parameter.
	CoreParamServer
	// CoreParamRequest is the handler-scoped Request handle.
	CoreParamRequest
	// CoreParamWriter is the handler-scoped Writer handle.
	CoreParamWriter
	// CoreParamAppPtr is a Ptr<App> parameter.
	CoreParamAppPtr
	// CoreParamHandler is Fun<(Ptr<App>, Request, Writer): Nil | Error>.
	CoreParamHandler
	// CoreParamSize is a Size parameter.
	CoreParamSize
	// CoreParamByteList is a List<Byte> the call appends to.
	CoreParamByteList
)

// CoreResult is one function's success shape. The Error union a fallible
// function adds at the call site is classified separately by ErrorBehavior.
type CoreResult uint8

const (
	CoreResultString CoreResult = iota
	CoreResultStringSlice
	CoreResultNil
	CoreResultSize
	// CoreResultValue is Json.Value | Error.
	CoreResultValue
	// CoreResultNoValue produces no value and can never fail.
	CoreResultNoValue
	// CoreResultPattern is Pattern | Error.
	CoreResultPattern
	// CoreResultBool is Bool | Error.
	CoreResultBool
	// CoreResultSpanNil is Span | Nil | Error.
	CoreResultSpanNil
	// CoreResultMatchNil is Match | Nil | Error.
	CoreResultMatchNil
	// CoreResultRouter is Router<App>, produced directly.
	CoreResultRouter
	// CoreResultServer is Server<App> | Error.
	CoreResultServer
	// CoreResultConfig is a ServerConfig record, produced directly.
	CoreResultConfig
	// CoreResultReadBody is Size | EoS | Error.
	CoreResultReadBody
	// CoreResultBytes is a read-only Slice<Byte>, produced directly.
	CoreResultBytes
	// CoreResultBytesNil is Slice<Byte> | Nil; it never fails.
	CoreResultBytesNil
	// CoreResultHeaders is a Slice<Header>, produced directly.
	CoreResultHeaders
)

// coreResultFallible reports whether a result shape unions with Error. The
// direct shapes -- a bare Size, nothing, and the std/http values a call builds
// without a failure path -- never do.
func coreResultFallible(result CoreResult) bool {
	switch result {
	case CoreResultSize, CoreResultNoValue, CoreResultRouter, CoreResultConfig, CoreResultBytes, CoreResultBytesNil, CoreResultHeaders:
		return false
	}
	return true
}

// ErrorBehavior classifies whether a function's result unions with the built-in
// Error type at the call site. It restates the error half of CoreResult as its
// own fact; a builtin function's error behavior belongs to the checker
// operation it routes to, so its record leaves this zero and validation checks
// the pair only for a runtime function.
type ErrorBehavior uint8

const (
	// ErrorNever yields its result directly, never an Error.
	ErrorNever ErrorBehavior = iota
	// ErrorFallible unions its result with the built-in Error type.
	ErrorFallible
)

// CoreFunction is one exported module function. A moved capability's static
// operation carries Builtin, the checker operation that resolves it. A
// core-library runtime function carries Runtime, the emitted C entry point
// (the RuntimeSymbol fact), and its Params and Result. Exactly one
// of Builtin and Runtime is set. The runtime template owns the function's
// stable ErrorKind and fixed message, so neither crosses this boundary.
//
// TypeParams is the number of explicit type arguments a generic function
// takes; Constructs names the exported type a constructor-shaped function
// builds, in which case Name is that type's own export name.
type CoreFunction struct {
	Name          string
	Builtin       string
	Params        []CoreParam
	Result        CoreResult
	ErrorBehavior ErrorBehavior
	Runtime       string
	Components    []ComponentID
	TypeParams    int
	Constructs    CoreTypeID
}

// CoreMethod is one instance method of a core-library type. Its embedded
// function record states the runtime call in C argument order, and the
// receiver occupies Params[ReceiverIndex]; the call site's own arguments fill
// the remaining slots in order.
type CoreMethod struct {
	Receiver      CoreTypeID
	ReceiverIndex int
	CoreFunction
}

// CallParams lists the parameters a call site writes: Params without the
// receiver slot.
func (method CoreMethod) CallParams() []CoreParam {
	params := make([]CoreParam, 0, len(method.Params)-1)
	params = append(params, method.Params[:method.ReceiverIndex]...)
	return append(params, method.Params[method.ReceiverIndex+1:]...)
}

// CoreTypeExport is one exported type name and the identifier of the canonical
// type it names.
type CoreTypeExport struct {
	Name   string
	TypeID CoreTypeID
}

// CoreGenericExport is one generic type a module exports. It resolves only
// through its type arguments, never to one Type, so it stays outside the
// concrete type graph; Arity is its argument count.
type CoreGenericExport struct {
	Name   string
	TypeID CoreTypeID
	Arity  int
}

// CoreModule is one core-library module: its canonical path and its exports.
// Methods are keyed by the receiver type they extend, which the module must
// export.
type CoreModule struct {
	ID        CoreModuleID
	Types     []CoreTypeExport
	Generics  []CoreGenericExport
	Functions []CoreFunction
	Methods   []CoreMethod
}

// The canonical type identifiers the export table references. Values name the
// canonical type so a record reads against the identity it resolves to.
const (
	CoreTypeIO                  CoreTypeID = "IO"
	CoreTypeBytes               CoreTypeID = "Bytes"
	CoreTypeSeek                CoreTypeID = "Seek"
	CoreTypeFile                CoreTypeID = "File"
	CoreTypeFileMode            CoreTypeID = "FileMode"
	CoreTypeDuration            CoreTypeID = "Duration"
	CoreTypeInstant             CoreTypeID = "Instant"
	CoreTypeWallTime            CoreTypeID = "WallTime"
	CoreTypeAddress             CoreTypeID = "Address"
	CoreTypeTcpConnection       CoreTypeID = "TcpConnection"
	CoreTypeTcpListener         CoreTypeID = "TcpListener"
	CoreTypeProcess             CoreTypeID = "Process"
	CoreTypePipe                CoreTypeID = "Pipe"
	CoreTypeProcessOptions      CoreTypeID = "ProcessOptions"
	CoreTypeStartedProcess      CoreTypeID = "StartedProcess"
	CoreTypeEnvironment         CoreTypeID = "Environment"
	CoreTypeEnvironmentVariable CoreTypeID = "EnvironmentVariable"
	CoreTypeProcessStream       CoreTypeID = "ProcessStream"
	CoreTypeExitStatus          CoreTypeID = "ExitStatus"
	CoreTypeSignal              CoreTypeID = "Signal"
	CoreTypeSignals             CoreTypeID = "Signals"
	CoreTypeTerminalSize        CoreTypeID = "TerminalSize"
	// CoreTypeJsonValue is std/json's exported union Value.
	CoreTypeJsonValue CoreTypeID = "JsonValue"
	// CoreTypeJsonMember is std/json's ordered object member record.
	CoreTypeJsonMember CoreTypeID = "JsonMember"
	// The std/http handles and records. Router and Server are generic
	// families: a Router<App> is identified by its App argument.
	CoreTypeHttpRequest      CoreTypeID = "HttpRequest"
	CoreTypeHttpWriter       CoreTypeID = "HttpWriter"
	CoreTypeHttpHeader       CoreTypeID = "HttpHeader"
	CoreTypeHttpServerConfig CoreTypeID = "HttpServerConfig"
	CoreTypeHttpRouter       CoreTypeID = "HttpRouter"
	CoreTypeHttpServer       CoreTypeID = "HttpServer"
	CoreTypeRegexPattern     CoreTypeID = "Pattern"
	CoreTypeRegexSpan        CoreTypeID = "Span"
	CoreTypeRegexMatch       CoreTypeID = "Match"
)

// httpRuntime is the demand of every std/http operation that needs the
// connection runtime: the server component and its private parser adapter.
var httpRuntime = []ComponentID{ComponentServer, ComponentHTTP}

// coreModules is the registry. It is unexported so no importer can rewrite a
// record, and every query clones the slices it returns, so a caller can never
// reach the registry's own backing arrays.
var coreModules = []CoreModule{
	{
		ID: "std/io",
		Types: []CoreTypeExport{
			{Name: "IO", TypeID: CoreTypeIO},
			{Name: "Bytes", TypeID: CoreTypeBytes},
			{Name: "Seek", TypeID: CoreTypeSeek},
		},
		Functions: []CoreFunction{
			{Name: "stdin", Builtin: "io_stdin"},
			{Name: "stdout", Builtin: "io_stdout"},
			{Name: "stderr", Builtin: "io_stderr"},
			{Name: "bytes_over", Builtin: "io_bytes_over"},
		},
	},
	{
		ID: "std/fs",
		Types: []CoreTypeExport{
			{Name: "File", TypeID: CoreTypeFile},
			{Name: "FileMode", TypeID: CoreTypeFileMode},
		},
		Functions: []CoreFunction{
			{Name: "open", Builtin: "file_open"},
		},
	},
	{
		ID: "std/time",
		Types: []CoreTypeExport{
			{Name: "Duration", TypeID: CoreTypeDuration},
			{Name: "Instant", TypeID: CoreTypeInstant},
			{Name: "WallTime", TypeID: CoreTypeWallTime},
		},
		Functions: []CoreFunction{
			{Name: "nanoseconds", Builtin: "time_nanoseconds"},
			{Name: "microseconds", Builtin: "time_microseconds"},
			{Name: "milliseconds", Builtin: "time_milliseconds"},
			{Name: "seconds", Builtin: "time_seconds"},
			{Name: "now", Builtin: "time_now"},
			{Name: "wall_time", Builtin: "time_wall_time"},
			{Name: "sleep", Builtin: "time_sleep"},
		},
	},
	{
		ID: "std/net",
		Types: []CoreTypeExport{
			{Name: "Address", TypeID: CoreTypeAddress},
			{Name: "TcpConnection", TypeID: CoreTypeTcpConnection},
			{Name: "TcpListener", TypeID: CoreTypeTcpListener},
		},
		Functions: []CoreFunction{
			{Name: "parse_address", Builtin: "address_parse"},
			{Name: "resolve", Builtin: "dns_resolve"},
			{Name: "connect", Builtin: "tcp_connect"},
			{Name: "listen", Builtin: "tcp_listen"},
		},
	},
	{
		ID: "std/process",
		Types: []CoreTypeExport{
			{Name: "Process", TypeID: CoreTypeProcess},
			{Name: "Pipe", TypeID: CoreTypePipe},
			{Name: "ProcessOptions", TypeID: CoreTypeProcessOptions},
			{Name: "StartedProcess", TypeID: CoreTypeStartedProcess},
			{Name: "Environment", TypeID: CoreTypeEnvironment},
			{Name: "EnvironmentVariable", TypeID: CoreTypeEnvironmentVariable},
			{Name: "ProcessStream", TypeID: CoreTypeProcessStream},
			{Name: "ExitStatus", TypeID: CoreTypeExitStatus},
		},
		Functions: []CoreFunction{
			{Name: "start", Builtin: "process_start"},
		},
	},
	{
		ID: "std/signal",
		Types: []CoreTypeExport{
			{Name: "Signal", TypeID: CoreTypeSignal},
			{Name: "Signals", TypeID: CoreTypeSignals},
		},
		Functions: []CoreFunction{
			{Name: "subscribe", Builtin: "signals_subscribe"},
		},
	},
	{
		ID: "std/terminal",
		Types: []CoreTypeExport{
			{Name: "TerminalSize", TypeID: CoreTypeTerminalSize},
		},
		Functions: []CoreFunction{
			{Name: "is_attached", Builtin: "terminal_is_attached"},
			{Name: "size", Builtin: "terminal_size"},
		},
	},
	{
		ID: "std/json",
		Types: []CoreTypeExport{
			{Name: "Value", TypeID: CoreTypeJsonValue},
			{Name: "Member", TypeID: CoreTypeJsonMember},
		},
		Functions: []CoreFunction{
			{Name: "parse", Params: []CoreParam{CoreParamHeap, CoreParamString}, Result: CoreResultValue, ErrorBehavior: ErrorFallible, Runtime: "hex_json_parse", Components: []ComponentID{ComponentJSON}},
		},
		Methods: []CoreMethod{
			{CoreTypeJsonValue, 1, CoreFunction{Name: "stringify", Params: []CoreParam{CoreParamHeap, CoreParamValue}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_json_stringify", Components: []ComponentID{ComponentJSON}}},
			{CoreTypeJsonValue, 1, CoreFunction{Name: "free", Params: []CoreParam{CoreParamHeap, CoreParamValue}, Result: CoreResultNoValue, ErrorBehavior: ErrorNever, Runtime: "hex_json_free", Components: []ComponentID{ComponentJSON}}},
		},
	},
	{
		ID: "std/regex",
		Types: []CoreTypeExport{
			{Name: "Span", TypeID: CoreTypeRegexSpan},
			{Name: "Match", TypeID: CoreTypeRegexMatch},
			{Name: "Pattern", TypeID: CoreTypeRegexPattern},
		},
		Functions: []CoreFunction{
			{Name: "compile", Params: []CoreParam{CoreParamHeap, CoreParamString}, Result: CoreResultPattern, ErrorBehavior: ErrorFallible, Runtime: "hex_regex_compile", Components: []ComponentID{ComponentRegex}},
		},
		Methods: []CoreMethod{
			{CoreTypeRegexPattern, 1, CoreFunction{Name: "test", Params: []CoreParam{CoreParamHeap, CoreParamPattern, CoreParamString}, Result: CoreResultBool, ErrorBehavior: ErrorFallible, Runtime: "hex_regex_test", Components: []ComponentID{ComponentRegex}}},
			{CoreTypeRegexPattern, 1, CoreFunction{Name: "find", Params: []CoreParam{CoreParamHeap, CoreParamPattern, CoreParamString}, Result: CoreResultSpanNil, ErrorBehavior: ErrorFallible, Runtime: "hex_regex_find", Components: []ComponentID{ComponentRegex}}},
			{CoreTypeRegexPattern, 1, CoreFunction{Name: "capture", Params: []CoreParam{CoreParamHeap, CoreParamPattern, CoreParamString}, Result: CoreResultMatchNil, ErrorBehavior: ErrorFallible, Runtime: "hex_regex_capture", Components: []ComponentID{ComponentRegex}}},
			{CoreTypeRegexPattern, 1, CoreFunction{Name: "free", Params: []CoreParam{CoreParamHeap, CoreParamPattern}, Result: CoreResultNoValue, ErrorBehavior: ErrorNever, Runtime: "hex_regex_free", Components: []ComponentID{ComponentRegex}}},
			{CoreTypeRegexMatch, 1, CoreFunction{Name: "free", Params: []CoreParam{CoreParamHeap, CoreParamMatch}, Result: CoreResultNoValue, ErrorBehavior: ErrorNever, Runtime: "hex_regex_free_match", Components: []ComponentID{ComponentRegex}}},
		},
	},
	{
		ID: "std/http",
		Types: []CoreTypeExport{
			{Name: "Request", TypeID: CoreTypeHttpRequest},
			{Name: "Writer", TypeID: CoreTypeHttpWriter},
			{Name: "Header", TypeID: CoreTypeHttpHeader},
			{Name: "ServerConfig", TypeID: CoreTypeHttpServerConfig},
		},
		Generics: []CoreGenericExport{
			{Name: "Router", TypeID: CoreTypeHttpRouter, Arity: 1},
			{Name: "Server", TypeID: CoreTypeHttpServer, Arity: 1},
		},
		Functions: []CoreFunction{
			{Name: "default_config", Params: []CoreParam{CoreParamString, CoreParamUInt16}, Result: CoreResultConfig, ErrorBehavior: ErrorNever, Runtime: "hex_http_default_config", Components: []ComponentID{ComponentServer}},
			{Name: "Router", Params: []CoreParam{CoreParamHeap}, Result: CoreResultRouter, ErrorBehavior: ErrorNever, Runtime: "hex_http_router_new", Components: []ComponentID{ComponentServer}, TypeParams: 1, Constructs: CoreTypeHttpRouter},
			{Name: "listen", Params: []CoreParam{CoreParamHeap, CoreParamConfig, CoreParamRouter, CoreParamAppPtr}, Result: CoreResultServer, ErrorBehavior: ErrorFallible, Runtime: "hex_http_listen", Components: httpRuntime, TypeParams: 1},
		},
		Methods: []CoreMethod{
			{CoreTypeHttpRouter, 0, CoreFunction{Name: "route", Params: []CoreParam{CoreParamRouter, CoreParamString, CoreParamString, CoreParamHandler}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_http_router_route", Components: []ComponentID{ComponentServer, ComponentHTTP}}},
			{CoreTypeHttpRouter, 0, CoreFunction{Name: "free", Params: []CoreParam{CoreParamRouter, CoreParamHeap}, Result: CoreResultNoValue, ErrorBehavior: ErrorNever, Runtime: "hex_http_router_free", Components: []ComponentID{ComponentServer}}},
			{CoreTypeHttpServer, 0, CoreFunction{Name: "run", Params: []CoreParam{CoreParamServer}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_http_server_run", Components: httpRuntime}},
			{CoreTypeHttpServer, 0, CoreFunction{Name: "wait", Params: []CoreParam{CoreParamServer}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_http_server_wait", Components: httpRuntime}},
			{CoreTypeHttpServer, 0, CoreFunction{Name: "stop", Params: []CoreParam{CoreParamServer}, Result: CoreResultNoValue, ErrorBehavior: ErrorNever, Runtime: "hex_http_server_stop", Components: httpRuntime}},
			{CoreTypeHttpServer, 0, CoreFunction{Name: "free", Params: []CoreParam{CoreParamServer, CoreParamHeap}, Result: CoreResultNoValue, ErrorBehavior: ErrorNever, Runtime: "hex_http_server_free", Components: httpRuntime}},
			{CoreTypeHttpRequest, 0, CoreFunction{Name: "method", Params: []CoreParam{CoreParamRequest}, Result: CoreResultBytes, ErrorBehavior: ErrorNever, Runtime: "hex_http_request_method", Components: httpRuntime}},
			{CoreTypeHttpRequest, 0, CoreFunction{Name: "target", Params: []CoreParam{CoreParamRequest}, Result: CoreResultBytes, ErrorBehavior: ErrorNever, Runtime: "hex_http_request_target", Components: httpRuntime}},
			{CoreTypeHttpRequest, 0, CoreFunction{Name: "path", Params: []CoreParam{CoreParamRequest}, Result: CoreResultBytes, ErrorBehavior: ErrorNever, Runtime: "hex_http_request_path", Components: httpRuntime}},
			{CoreTypeHttpRequest, 0, CoreFunction{Name: "headers", Params: []CoreParam{CoreParamRequest}, Result: CoreResultHeaders, ErrorBehavior: ErrorNever, Runtime: "hex_http_request_headers", Components: httpRuntime}},
			{CoreTypeHttpRequest, 0, CoreFunction{Name: "header", Params: []CoreParam{CoreParamRequest, CoreParamString}, Result: CoreResultBytesNil, ErrorBehavior: ErrorNever, Runtime: "hex_http_request_header", Components: httpRuntime}},
			{CoreTypeHttpRequest, 0, CoreFunction{Name: "read", Params: []CoreParam{CoreParamRequest, CoreParamByteList, CoreParamSize}, Result: CoreResultReadBody, ErrorBehavior: ErrorFallible, Runtime: "hex_http_request_read", Components: httpRuntime}},
			{CoreTypeHttpWriter, 0, CoreFunction{Name: "status", Params: []CoreParam{CoreParamWriter, CoreParamUInt16}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_http_writer_status", Components: httpRuntime}},
			{CoreTypeHttpWriter, 0, CoreFunction{Name: "header", Params: []CoreParam{CoreParamWriter, CoreParamString, CoreParamBytes}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_http_writer_header", Components: httpRuntime}},
			{CoreTypeHttpWriter, 0, CoreFunction{Name: "content_length", Params: []CoreParam{CoreParamWriter, CoreParamSize}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_http_writer_content_length", Components: httpRuntime}},
			{CoreTypeHttpWriter, 0, CoreFunction{Name: "write", Params: []CoreParam{CoreParamWriter, CoreParamBytes}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_http_writer_write", Components: httpRuntime}},
		},
	},
	{
		ID: "std/program",
		Functions: []CoreFunction{
			{Name: "arguments", Result: CoreResultStringSlice, ErrorBehavior: ErrorFallible, Runtime: "hex_program_arguments", Components: []ComponentID{ComponentProgram}},
			{Name: "current_directory", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_current_directory", Components: []ComponentID{ComponentProgram}},
			{Name: "home_directory", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_home_directory", Components: []ComponentID{ComponentProgram}},
			{Name: "temporary_directory", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_temporary_directory", Components: []ComponentID{ComponentProgram}},
			{Name: "executable_path", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_executable_path", Components: []ComponentID{ComponentProgram}},
			{Name: "available_parallelism", Result: CoreResultSize, ErrorBehavior: ErrorNever, Runtime: "hex_program_available_parallelism", Components: []ComponentID{ComponentProgram}},
		},
	},
	{
		ID: "std/entropy",
		Functions: []CoreFunction{
			{Name: "fill", Params: []CoreParam{CoreParamMutByteSlice}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_entropy_fill", Components: []ComponentID{ComponentEntropy}},
		},
	},
}

// CoreModuleKnown reports whether id names a registered module, without
// allocating a record copy.
func CoreModuleKnown(id CoreModuleID) bool {
	for _, module := range coreModules {
		if module.ID == id {
			return true
		}
	}
	return false
}

// CoreModuleByID resolves one core-library module by canonical path.
func CoreModuleByID(id CoreModuleID) (CoreModule, bool) {
	for _, module := range coreModules {
		if module.ID == id {
			return cloneCoreModule(module), true
		}
	}
	return CoreModule{}, false
}

// CoreModules returns every module record in registration order as a copy.
func CoreModules() []CoreModule {
	modules := make([]CoreModule, len(coreModules))
	for index, module := range coreModules {
		modules[index] = cloneCoreModule(module)
	}
	return modules
}

// CoreFunctionByModuleName resolves one exported function of a known module.
func CoreFunctionByModuleName(id CoreModuleID, name string) (CoreFunction, bool) {
	for _, module := range coreModules {
		if module.ID != id {
			continue
		}
		for _, function := range module.Functions {
			if function.Name == name {
				return cloneCoreFunction(function), true
			}
		}
	}
	return CoreFunction{}, false
}

// CoreMethodsNamed returns every core-library method called name, one per
// receiver type that declares it, for the adapter to match against the call's
// receiver.
func CoreMethodsNamed(name string) []CoreMethod {
	var found []CoreMethod
	for _, module := range coreModules {
		for _, method := range module.Methods {
			if method.Name == name {
				method.CoreFunction = cloneCoreFunction(method.CoreFunction)
				found = append(found, method)
			}
		}
	}
	return found
}

// CoreFunctionByRuntime resolves one emitted C entry point back to its owning
// module and declaration, methods included. validateCorelib enforces that
// runtime entry points are unique across every core-library function and
// method, so the first match is the only match.
func CoreFunctionByRuntime(runtime string) (CoreModuleID, CoreFunction, bool) {
	for _, module := range coreModules {
		for _, function := range module.Functions {
			if function.Runtime == runtime {
				return module.ID, cloneCoreFunction(function), true
			}
		}
		for _, method := range module.Methods {
			if method.Runtime == runtime {
				return module.ID, cloneCoreFunction(method.CoreFunction), true
			}
		}
	}
	return "", CoreFunction{}, false
}

// cloneCoreModule deep-copies the slices a record owns, so a query result
// shares no backing array with the registry.
func cloneCoreModule(module CoreModule) CoreModule {
	module.Types = append([]CoreTypeExport(nil), module.Types...)
	functions, methods := module.Functions, module.Methods
	module.Functions = make([]CoreFunction, len(functions))
	for index, function := range functions {
		module.Functions[index] = cloneCoreFunction(function)
	}
	module.Methods = make([]CoreMethod, len(methods))
	for index, method := range methods {
		module.Methods[index] = CoreMethod{method.Receiver, method.ReceiverIndex, cloneCoreFunction(method.CoreFunction)}
	}
	return module
}

// cloneCoreFunction deep-copies one function record's parameter and component
// slices.
func cloneCoreFunction(function CoreFunction) CoreFunction {
	function.Params = append([]CoreParam(nil), function.Params...)
	function.Components = append([]ComponentID(nil), function.Components...)
	return function
}

// validateCorelib checks one core-library registry for internal consistency:
// unique module paths; unique export names inside a module; exactly one of a
// builtin and a runtime entry point per function; methods that extend an
// exported type, once per receiver and name, with a receiver slot; runtime
// entry points unique across every module; and, for each
// runtime function or method, a valid error behavior
// consistent with its result and at least one known component demand. It
// cannot check that a type identifier resolves, because resolution crosses the
// import boundary; the corelib adapter's own test covers that half. It takes
// the slice so a test can validate a crafted registry without mutating the
// package's own.
func validateCorelib(modules []CoreModule) error {
	moduleIDs := make(map[CoreModuleID]bool, len(modules))
	runtimes := make(map[string]CoreModuleID)
	for _, module := range modules {
		if module.ID == "" {
			return fmt.Errorf("specdata/corelib: module has an empty id")
		}
		if moduleIDs[module.ID] {
			return fmt.Errorf("specdata/corelib: module %q is declared twice", module.ID)
		}
		moduleIDs[module.ID] = true
		methodNames := make(map[string]bool, len(module.Methods))
		exports := make(map[string]bool, len(module.Types)+len(module.Functions))
		for _, export := range module.Types {
			if export.Name == "" {
				return fmt.Errorf("specdata/corelib: module %q exports a type with an empty name", module.ID)
			}
			if export.TypeID == "" {
				return fmt.Errorf("specdata/corelib: module %q type %q has an empty identifier", module.ID, export.Name)
			}
			if exports[export.Name] {
				return fmt.Errorf("specdata/corelib: module %q exports %q twice", module.ID, export.Name)
			}
			exports[export.Name] = true
		}
		for _, function := range module.Functions {
			if function.Name == "" {
				return fmt.Errorf("specdata/corelib: module %q exports a function with an empty name", module.ID)
			}
			if exports[function.Name] && function.Constructs == "" {
				return fmt.Errorf("specdata/corelib: module %q exports %q twice", module.ID, function.Name)
			}
			if function.Constructs != "" {
				if !moduleExportsType(module, function.Constructs) {
					return fmt.Errorf("specdata/corelib: constructor %q.%q builds a type the module does not export", module.ID, function.Name)
				}
			} else {
				exports[function.Name] = true
			}
			if err := validateCoreFunction(module.ID, function, runtimes); err != nil {
				return err
			}
		}
		for _, method := range module.Methods {
			if method.Name == "" {
				return fmt.Errorf("specdata/corelib: module %q declares a method with an empty name", module.ID)
			}
			if !moduleExportsType(module, method.Receiver) {
				return fmt.Errorf("specdata/corelib: method %q.%q receiver %q is not an exported type", module.ID, method.Name, method.Receiver)
			}
			key := string(method.Receiver) + "." + method.Name
			if methodNames[key] {
				return fmt.Errorf("specdata/corelib: module %q declares method %q twice", module.ID, key)
			}
			methodNames[key] = true
			if method.Runtime == "" || method.ReceiverIndex < 0 || method.ReceiverIndex >= len(method.Params) {
				return fmt.Errorf("specdata/corelib: method %q.%q must name a runtime entry point with a receiver slot", module.ID, method.Name)
			}
			if err := validateCoreFunction(module.ID, method.CoreFunction, runtimes); err != nil {
				return err
			}
		}
	}
	return nil
}

// moduleExportsType reports whether module exports a type with identifier id.
func moduleExportsType(module CoreModule, id CoreTypeID) bool {
	for _, export := range module.Types {
		if export.TypeID == id {
			return true
		}
	}
	for _, export := range module.Generics {
		if export.TypeID == id {
			return true
		}
	}
	return false
}

// validateCoreFunction checks the facts shared by a module function and a
// method: a valid error behavior, exactly one entry point kind, and, for a
// runtime entry point, a unique symbol, an error behavior consistent with the
// result, and known components.
func validateCoreFunction(id CoreModuleID, function CoreFunction, runtimes map[string]CoreModuleID) error {
	if function.ErrorBehavior != ErrorNever && function.ErrorBehavior != ErrorFallible {
		return fmt.Errorf("specdata/corelib: function %q.%q has unknown error behavior", id, function.Name)
	}
	if (function.Builtin == "") == (function.Runtime == "") {
		return fmt.Errorf("specdata/corelib: function %q.%q must name exactly one of a builtin or a runtime entry point", id, function.Name)
	}
	// A builtin routes to a checker operation that owns its error
	// behavior and component demand; only a runtime function states
	// both here.
	if function.Builtin != "" {
		return nil
	}
	if owner, ok := runtimes[function.Runtime]; ok {
		return fmt.Errorf("specdata/corelib: runtime %q is declared by %q and %q", function.Runtime, owner, id)
	}
	runtimes[function.Runtime] = id
	if (function.ErrorBehavior == ErrorFallible) != coreResultFallible(function.Result) {
		return fmt.Errorf("specdata/corelib: runtime %q.%q result and error behavior disagree", id, function.Name)
	}
	if len(function.Components) == 0 {
		return fmt.Errorf("specdata/corelib: runtime %q.%q names no component", id, function.Name)
	}
	seenComponents := make(map[ComponentID]bool, len(function.Components))
	for _, component := range function.Components {
		if _, known := Component(component); !known {
			return fmt.Errorf("specdata/corelib: runtime %q.%q names unknown component %q", id, function.Name, component)
		}
		if seenComponents[component] {
			return fmt.Errorf("specdata/corelib: runtime %q.%q repeats component %q", id, function.Name, component)
		}
		seenComponents[component] = true
	}
	return nil
}
