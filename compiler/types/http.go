package types

import "hexal/compiler/specdata"

// std/http's data model: the opaque Request and Writer handles a handler
// borrows for one invocation, the ordinary Header and ServerConfig records,
// and the two generic opaque handles Router<App> and Server<App>. The App
// argument exists only in the checker: every Router<App> is one C pointer
// type and every Server<App> another, so the C representation never depends
// on App. None of the opaque handles has equality, ordering, printing, or
// Dict-key eligibility.

var httpModelData = buildHTTPModel()

type httpModeler struct {
	request     Type
	writer      Type
	files       Type
	header      Type
	config      Type
	bytes       Type
	headerSlice Type
}

// HttpGenericInfo is the metadata of one generic std/http handle: its family
// and the application type its handlers receive a pointer to.
type HttpGenericInfo struct {
	Family specdata.TypeID
	App    Type
}

// HttpRequestType is the handler-scoped request handle.
func HttpRequestType() Type { return httpModelData.request }

// HttpWriterType is the handler-scoped response writer handle.
func HttpWriterType() Type { return httpModelData.writer }

// HttpFileServerType is the opaque static-file server handle.
func HttpFileServerType() Type { return httpModelData.files }

// IsHttpFileServer reports whether typ is the canonical FileServer handle.
func IsHttpFileServer(typ Type) bool {
	return typ.identity != nil && typ.identity == httpModelData.files.identity
}

// HttpHeaderType is the ordinary record of one borrowed header field.
func HttpHeaderType() Type { return httpModelData.header }

// HttpServerConfigType is the ordinary record of server construction settings.
func HttpServerConfigType() Type { return httpModelData.config }

// HttpByteSliceType is the read-only byte Slice the Header record's members
// and the request views spell.
func HttpByteSliceType() Type { return httpModelData.bytes }

// HttpHeaderSliceType is the Slice<Header> Request.headers returns.
func HttpHeaderSliceType() Type { return httpModelData.headerSlice }

// IsHttpRequest and IsHttpWriter report whether typ is the canonical handle.
func IsHttpRequest(typ Type) bool {
	return typ.identity != nil && typ.identity == httpModelData.request.identity
}

func IsHttpWriter(typ Type) bool {
	return typ.identity != nil && typ.identity == httpModelData.writer.identity
}

// IsHttpHeader and IsHttpServerConfig report whether typ is one of the
// std/http records whose C definition lives in a core component header
// (hexal/slice.h and hexal/server.h) rather than a module header.
func IsHttpHeader(typ Type) bool {
	return typ.identity != nil && typ.identity == httpModelData.header.identity
}

func IsHttpServerConfig(typ Type) bool {
	return typ.identity != nil && typ.identity == httpModelData.config.identity
}

// IsHttpRouter and IsHttpServer report whether typ is a Router<App> or a
// Server<App> of any App.
func IsHttpRouter(typ Type) bool {
	return typ.HttpGeneric != nil && typ.HttpGeneric.Family == specdata.TypeHttpRouter
}

func IsHttpServer(typ Type) bool {
	return typ.HttpGeneric != nil && typ.HttpGeneric.Family == specdata.TypeHttpServer
}

// HttpGenericApp returns the App argument of a Router<App> or Server<App>.
func HttpGenericApp(typ Type) (Type, bool) {
	if typ.HttpGeneric == nil {
		return Type{}, false
	}
	return typ.HttpGeneric.App, true
}

// HttpGenericFamily returns the registry identifier of a generic std/http
// handle type, the receiver identity its methods are keyed by.
func HttpGenericFamily(typ Type) (specdata.TypeID, bool) {
	if typ.HttpGeneric == nil {
		return "", false
	}
	return typ.HttpGeneric.Family, true
}

func staticSliceType(element Type, cName string) Type {
	canonicalKey := "slice:" + element.CanonicalKey
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	return Type{
		Name:         "Slice<" + element.Name + ">",
		CName:        cName,
		CanonicalKey: canonicalKey,
		Slice:        &SliceInfo{Element: element},
		identity:     identity,
	}
}

func buildHTTPModel() httpModeler {
	request := Type{
		Name:         "Request",
		CName:        "hex_http_request",
		CanonicalKey: "HttpRequest",
		identity:     newTypeIdentity(),
	}
	writer := Type{
		Name:         "Writer",
		CName:        "hex_http_writer",
		CanonicalKey: "HttpWriter",
		identity:     newTypeIdentity(),
	}
	files := Type{
		Name:         "FileServer",
		CName:        "hex_http_files",
		CanonicalKey: "HttpFileServer",
		identity:     newTypeIdentity(),
	}
	bytes := staticSliceType(UInt8, "hex_slice_UInt8")
	header := builtinObject("Header", "hex_t_Header", []ObjectMember{
		{Name: "name", Type: bytes, Use: NewTypeUse(bytes)},
		{Name: "value", Type: bytes, Use: NewTypeUse(bytes)},
	})
	headerSlice := staticSliceType(header, "hex_slice_Header")
	member := func(name string, typ Type) ObjectMember {
		return ObjectMember{Name: name, Type: typ, Use: NewTypeUse(typ), Mutable: true}
	}
	config := builtinObject("ServerConfig", "hex_t_ServerConfig", []ObjectMember{
		member("host", StringType),
		member("port", UInt16),
		member("max_request_line_bytes", SizeType),
		member("max_header_bytes", SizeType),
		member("max_header_count", SizeType),
		member("max_body_bytes", SizeType),
		member("max_trailer_bytes", SizeType),
		member("receive_buffer_bytes", SizeType),
		member("write_buffer_bytes", SizeType),
		member("max_connections", SizeType),
		member("backlog", SizeType),
		member("header_timeout", DurationType),
		member("body_timeout", DurationType),
		member("write_timeout", DurationType),
		member("idle_timeout", DurationType),
		member("shutdown_timeout", DurationType),
		member("tcp_nodelay", Bool),
	})
	return httpModeler{request: request, writer: writer, files: files, header: header, config: config, bytes: bytes, headerSlice: headerSlice}
}

// httpGenericType constructs or retrieves one canonical generic std/http
// handle. app must be a canonical complete value type for the environment; a
// type parameter defers to specialization like every other generic position.
func (environment *Environment) httpGenericType(family specdata.TypeID, name, cName string, app Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, app, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) {
		return Type{}
	}
	canonicalKey := "http-" + string(family) + ":" + app.CanonicalKey
	if cached, ok := environment.arena.httpTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         name + "<" + app.Name + ">",
		CName:        cName,
		CanonicalKey: canonicalKey,
		HttpGeneric:  &HttpGenericInfo{Family: family, App: app},
		identity:     identity,
	}
	environment.arena.httpTypes[canonicalKey] = typ
	return typ
}

// HttpRouterType constructs or retrieves the canonical Router<App> handle.
func (environment *Environment) HttpRouterType(app Type) Type {
	return environment.httpGenericType(specdata.TypeHttpRouter, "Router", "hex_http_router", app)
}

// HttpServerType constructs or retrieves the canonical Server<App> handle.
func (environment *Environment) HttpServerType(app Type) Type {
	return environment.httpGenericType(specdata.TypeHttpServer, "Server", "hex_http_server", app)
}
