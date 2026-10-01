package generator

import (
	"hexal/compiler/checker"
	"hexal/compiler/config"
	"hexal/compiler/corelib"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// std/http generation: the server component pair (server.h/.c: the config
// record, the router, and the connection state machine) and the private
// parser adapter pair (http.h/.c, the only translation unit including
// <llhttp.h>). Demand reads the CorelibCallExpression nodes the checker
// resolved through the std/http registry: naming only Request, Writer,
// Header, Router, or Server selects nothing, a call selects the server
// component, and the parser adapter and its native dependency are selected
// exactly when a route or a listening server is reachable.

// generatedServerState records one program's std/http reachability. used
// selects the hexal/server.h declarations any named std/http type or call
// needs; calls selects the server runtime itself, so a program that only names
// Request, Writer, Header, or ServerConfig links no server code.
type generatedServerState struct {
	used  bool
	calls bool
	// config, router, route, and free gate the raw entry points one program
	// reaches.
	config bool
	router bool
	route  bool
	free   bool
	// runtime is set by every operation that needs the connection runtime:
	// listen, the Server operations, and the Request and Writer operations. It
	// selects the Task-aware network, scheduler, and clock machinery.
	runtime bool
	// files is set by the static file server operations and router mounts: it
	// selects the file server component and the router's mount table.
	files bool
	// parser is set by every operation that validates a method token or
	// parses a request: route registration and the listening server.
	parser   bool
	adapters []corelibAdapter
	// fileLiteral is the module source key's Error file literal, interned
	// only when an adapter exists to build a failure from it.
	fileLiteral literalHandle
}

// discoverGeneratedServer walks one module for std/http module calls.
func discoverGeneratedServer(program checker.Program, logicalKey string, literals *literalRegistry) *generatedServerState {
	state := &generatedServerState{}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsHttpRequest(typ) || compilerTypes.IsHttpWriter(typ) || compilerTypes.IsHttpHeader(typ) ||
				compilerTypes.IsHttpServerConfig(typ) || compilerTypes.IsHttpRouter(typ) || compilerTypes.IsHttpServer(typ) {
				state.used = true
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.CorelibCallExpression {
				return nil
			}
			path, function, ok := corelib.FunctionByRuntime(node.Name)
			if !ok {
				return unknownExpressionDiagnostic()
			}
			if path != "std/http" {
				return nil
			}
			state.used = true
			state.calls = true
			switch node.Name {
			case "hex_http_default_config":
				state.config = true
			case "hex_http_router_new":
				state.router = true
			case "hex_http_router_route":
				state.route = true
			case "hex_http_router_free":
				state.free = true
			case "hex_http_router_mount", "hex_http_files_open", "hex_http_files_serve_file", "hex_http_files_serve_directory", "hex_http_files_free":
				// The mount table shares the route registration's validation and
				// attachment helpers, and serving needs the connection runtime.
				state.files = true
				state.route = true
				state.runtime = true
				state.router = true
			default:
				// Every other registered operation belongs to the connection
				// runtime, which owns the router record it dispatches over.
				state.runtime = true
				state.router = true
			}
			// A route or a free operates on the router record the constructor
			// defines, so either one selects that definition.
			state.router = state.router || state.route || state.free
			for _, component := range function.Components {
				if component == specdata.ComponentHTTP {
					state.parser = true
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return state
	}
	state.adapters = discoverModuleAddonAdapters(program, "std/http")
	if len(state.adapters) > 0 {
		state.fileLiteral = literals.Intern(logicalKey)
	}
	return state
}

// httpServerComponents returns the server component pair when any std/http
// call is reachable.
func httpServerComponents(merged *programEmission) ([]componentArtifact, error) {
	state := merged.serverState
	if state == nil || !state.used {
		return nil, nil
	}
	model := serverSourceModel{
		Program:             state,
		MaxRequestLineBytes: config.HTTPMaxRequestLineBytes,
		MaxHeaderBytes:      config.HTTPMaxHeaderBytes,
		MaxHeaderCount:      config.HTTPMaxHeaderCount,
		MaxBodyBytes:        config.HTTPMaxBodyBytes,
		MaxTrailerBytes:     config.HTTPMaxTrailerBytes,
		ReceiveBufferBytes:  config.HTTPReceiveBufferBytes,
		WriteBufferBytes:    config.HTTPWriteBufferBytes,
		MaxConnections:      config.HTTPMaxConnections,
		Backlog:             config.HTTPBacklog,
		HeaderTimeout:       uint64(config.HTTPHeaderTimeout),
		BodyTimeout:         uint64(config.HTTPBodyTimeout),
		WriteTimeout:        uint64(config.HTTPWriteTimeout),
		IdleTimeout:         uint64(config.HTTPIdleTimeout),
		ShutdownTimeout:     uint64(config.HTTPShutdownTimeout),
		TCPNoDelay:          config.HTTPTCPNoDelay,
		ResponseHeadBytes:   config.HTTPResponseHeadBytes,
		LingerTimeout:       uint64(config.HTTPLingerTimeout),
		PollInterval:        uint64(config.HTTPPollInterval),
	}
	artifacts := []componentArtifact{{key: "hexal/server.h", template: "server.h", model: model}}
	if state.calls {
		artifacts = append(artifacts, componentArtifact{key: "hexal/server.c", template: "server.c", model: model})
	}
	return artifacts, nil
}

// httpParserComponents returns the private parser adapter pair when a route or
// a listening server is reachable; it renders no template directive, so the
// model is empty.
func httpParserComponents(merged *programEmission) ([]componentArtifact, error) {
	if !httpParserSelected(merged) {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/http.h", template: "http.h", model: struct{}{}},
		{key: "hexal/http.c", template: "http.c", model: struct{}{}},
	}, nil
}

// httpFilesComponents returns the static file server pair when a file server
// operation or a mount is reachable; it renders no template directive.
func httpFilesComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.serverState == nil || !merged.serverState.files {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/fileserver.h", template: "fileserver.h", model: struct{}{}},
		{key: "hexal/fileserver.c", template: "fileserver.c", model: struct{}{}},
	}, nil
}

// httpParserSelected reports whether the parser adapter and the pinned llhttp
// archive are reachable.
func httpParserSelected(merged *programEmission) bool {
	return merged != nil && merged.serverState != nil && merged.serverState.parser
}

// serverSourceModel carries the program state and the ServerConfig defaults the
// server component renders from the compiler configuration, the single owner
// of every default value. Durations are nanoseconds.
type serverSourceModel struct {
	Program             *generatedServerState
	MaxRequestLineBytes int
	MaxHeaderBytes      int
	MaxHeaderCount      int
	MaxBodyBytes        int
	MaxTrailerBytes     int
	ReceiveBufferBytes  int
	WriteBufferBytes    int
	MaxConnections      int
	Backlog             int
	HeaderTimeout       uint64
	BodyTimeout         uint64
	WriteTimeout        uint64
	IdleTimeout         uint64
	ShutdownTimeout     uint64
	TCPNoDelay          bool
	ResponseHeadBytes   int
	LingerTimeout       uint64
	PollInterval        uint64
}

// mergeServerInto unions one module's std/http demand into the program state.
func mergeServerInto(merged, state *generatedServerState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.calls = merged.calls || state.calls
	merged.config = merged.config || state.config
	merged.router = merged.router || state.router
	merged.route = merged.route || state.route
	merged.free = merged.free || state.free
	merged.runtime = merged.runtime || state.runtime
	merged.files = merged.files || state.files
	merged.parser = merged.parser || state.parser
}

// moduleServerComponent selects the server component header one module's
// checker reaches.
func moduleServerComponent(emission *moduleEmission) []string {
	if emission.serverState == nil || !emission.serverState.used {
		return nil
	}
	if emission.serverState.files {
		return []string{"hexal/server.h", "hexal/fileserver.h"}
	}
	return []string{"hexal/server.h"}
}

// The server component's templates read the reachable raw entry points through
// these accessors, since the program state's fields are unexported. Each gates
// one raw entry point so it renders only when its operation is reachable.
func (model serverSourceModel) Calls() bool       { return model.Program.calls }
func (model serverSourceModel) NeedConfig() bool  { return model.Program.config }
func (model serverSourceModel) NeedRouter() bool  { return model.Program.router }
func (model serverSourceModel) NeedRoute() bool   { return model.Program.route }
func (model serverSourceModel) NeedFree() bool    { return model.Program.free }
func (model serverSourceModel) NeedRuntime() bool { return model.Program.runtime }
func (model serverSourceModel) NeedFiles() bool   { return model.Program.files }
func (model serverSourceModel) NeedParser() bool  { return model.Program.parser }
