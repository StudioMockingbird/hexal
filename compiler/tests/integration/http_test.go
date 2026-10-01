package integration

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/compiler/config"
)

const httpHandlerPrelude = "import\n    Http from std.http\nend\n" +
	"type App is struct\n    hits: Int32\nend\n" +
	"fun home(app: Ptr<App>, request: Http.Request, response: Http.Writer): Nil | Error do\n    return nil\nend\n"

func TestHttpRouterSurfaceAndPrivateParserBoundary(t *testing.T) {
	source := httpHandlerPrelude +
		"fun build(h: Heap): Nil | Error do\n" +
		"    let mut router = Http.Router<App>(h)\n" +
		"    try router.route(\"GET\", \"/\", home)\n" +
		"    router.free(h)\n" +
		"    return nil\nend\n"
	result := assertCompiles(t, source)
	if !slices.Contains(result.Dependencies, compiler.RuntimeLlhttp) {
		t.Fatalf("route registration dependencies = %v, want llhttp", result.Dependencies)
	}
	adapter := moduleFile(t, result, "hexal/http.c")
	server := moduleFile(t, result, "hexal/server.c")
	if strings.Count(adapter, "#include \"llhttp.h\"") != 1 {
		t.Fatalf("llhttp must be included by exactly one private adapter:\n%s", adapter)
	}
	for name, content := range result.Files {
		if strings.HasSuffix(name, ".h") && strings.Contains(content, "llhttp.h") {
			t.Errorf("header %s includes llhttp:\n%s", name, content)
		}
		if strings.HasSuffix(name, ".c") && name != "hexal/http.c" && strings.Contains(content, "llhttp_") {
			t.Errorf("llhttp call escaped the private adapter into %s", name)
		}
	}
	if !strings.Contains(server, "hex_http_method_known") {
		t.Fatalf("route registration must validate the method through the parser adapter:\n%s", server)
	}
}

func TestHttpConfigDefaultsComeFromConfiguration(t *testing.T) {
	source := "import\n    Http from std.http\nend\n" +
		"let config = Http.default_config(\"127.0.0.1\", 8080)\n" +
		"print(config.port, \"\\n\")\n"
	result := assertCompiles(t, source)
	server := moduleFile(t, result, "hexal/server.c")
	for _, want := range []string{
		".hex_m_max_request_line_bytes = " + strconv.Itoa(config.HTTPMaxRequestLineBytes),
		".hex_m_max_header_bytes = " + strconv.Itoa(config.HTTPMaxHeaderBytes),
		".hex_m_max_header_count = " + strconv.Itoa(config.HTTPMaxHeaderCount),
		".hex_m_max_body_bytes = " + strconv.Itoa(config.HTTPMaxBodyBytes),
		".hex_m_max_trailer_bytes = " + strconv.Itoa(config.HTTPMaxTrailerBytes),
		".hex_m_receive_buffer_bytes = " + strconv.Itoa(config.HTTPReceiveBufferBytes),
		".hex_m_write_buffer_bytes = " + strconv.Itoa(config.HTTPWriteBufferBytes),
		".hex_m_max_connections = " + strconv.Itoa(config.HTTPMaxConnections),
		".hex_m_backlog = " + strconv.Itoa(config.HTTPBacklog),
		".hex_m_header_timeout = " + strconv.FormatInt(int64(config.HTTPHeaderTimeout), 10),
		".hex_m_body_timeout = " + strconv.FormatInt(int64(config.HTTPBodyTimeout), 10),
		".hex_m_write_timeout = " + strconv.FormatInt(int64(config.HTTPWriteTimeout), 10),
		".hex_m_idle_timeout = " + strconv.FormatInt(int64(config.HTTPIdleTimeout), 10),
		".hex_m_shutdown_timeout = " + strconv.FormatInt(int64(config.HTTPShutdownTimeout), 10),
		".hex_m_tcp_nodelay = true",
	} {
		if !strings.Contains(server, want) {
			t.Errorf("default_config does not render %q:\n%s", want, server)
		}
	}
	if slices.Contains(result.Dependencies, compiler.RuntimeLlhttp) {
		t.Fatalf("default_config alone dependencies = %v, want no llhttp", result.Dependencies)
	}
	if _, parser := result.Files["hexal/http.c"]; parser {
		t.Fatal("default_config alone must not select the parser adapter")
	}
}

func TestHttpTypeOnlyUseSelectsNoServerRuntime(t *testing.T) {
	source := "import\n    Http from std.http\nend\n" +
		"fun method_name(request: Http.Request, writer: Http.Writer, header: Http.Header, config: Http.ServerConfig) do\nend\n"
	result := assertCompiles(t, source)
	if slices.Contains(result.Dependencies, compiler.RuntimeLlhttp) {
		t.Fatalf("type-only dependencies = %v, want no llhttp", result.Dependencies)
	}
	if _, runtime := result.Files["hexal/server.c"]; runtime {
		t.Fatal("naming std/http types must not select the server runtime")
	}
	if _, parser := result.Files["hexal/http.c"]; parser {
		t.Fatal("naming std/http types must not select the parser adapter")
	}
}

func TestHttpHeaderIsAnOrdinaryRecordOfByteSlices(t *testing.T) {
	source := "import\n    Http from std.http\nend\n" +
		"fun first(headers: Slice<Http.Header>, index: Size): Slice<Byte> do\n" +
		"    return headers[index].name\nend\n"
	result := assertCompiles(t, source)
	slice := moduleFile(t, result, "hexal/slice.h")
	if strings.Count(slice, "typedef struct hex_t_Header") != 1 {
		t.Fatalf("Header record must be defined once beside its slice:\n%s", slice)
	}
	if strings.Index(slice, "hex_slice_UInt8;") > strings.Index(slice, "typedef struct hex_t_Header") {
		t.Fatalf("Header record must follow the byte slice it holds:\n%s", slice)
	}
}

func TestHttpRouterRejectsInvalidUse(t *testing.T) {
	routerPrelude := httpHandlerPrelude + "fun build(h: Heap): Nil | Error do\n    let mut router = Http.Router<App>(h)\n"
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"missing type argument", "import\n    Http from std.http\nend\nlet h = Heap()\nlet router = Http.Router(h)\n", "type argument"},
		{"handler with another context", httpHandlerPrelude +
			"type Other is struct\n    id: Int32\nend\n" +
			"fun build(h: Heap): Nil | Error do\n    let mut router = Http.Router<Other>(h)\n    try router.route(\"GET\", \"/\", home)\n    return nil\nend\n", "Fun"},
		{"handler that is not a function", routerPrelude + "    try router.route(\"GET\", \"/\", 3)\n    return nil\nend\n", "Fun"},
		{"route without method", routerPrelude + "    try router.route(\"/\", home)\n    return nil\nend\n", "argument"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) { assertRejects(t, testCase.source, testCase.want) })
	}
}

const httpServerPrelude = "import\n    Http from std.http\nend\n" +
	"type App is struct\n    hits: Atomic<Int32>\nend\n" +
	"fun home(app: Ptr<App>, request: Http.Request, response: Http.Writer): Nil | Error do\n    return nil\nend\n"

func TestHttpServerSurfaceCompilesAndSelectsTheRuntime(t *testing.T) {
	source := httpServerPrelude +
		"fun serve(heap: Heap): Nil | Error do\n" +
		"    let app = App(hits = Atomic<Int32>(0))\n" +
		"    let mut router = Http.Router<App>(heap)\n" +
		"    try router.route(\"GET\", \"/\", home)\n" +
		"    let mut config = Http.default_config(\"127.0.0.1\", 8080)\n" +
		"    config.max_body_bytes = 1024\n" +
		"    let server = try Http.listen<App>(heap, config, router, @app)\n" +
		"    server.stop()\n" +
		"    try server.run()\n" +
		"    try server.wait()\n" +
		"    server.free(heap)\n" +
		"    router.free(heap)\n" +
		"    return nil\nend\n"
	result := assertCompiles(t, source)
	for _, dependency := range []compiler.RuntimeDependency{compiler.RuntimeLlhttp, compiler.RuntimeLibuv} {
		if !slices.Contains(result.Dependencies, dependency) {
			t.Errorf("serving dependencies = %v, want %s", result.Dependencies, dependency)
		}
	}
	for _, key := range []string{"hexal/server.c", "hexal/http.c", "hexal/network.c", "hexal/concurrency.c", "hexal/handle.c", "hexal/event.c"} {
		if _, present := result.Files[key]; !present {
			t.Errorf("serving does not select %s", key)
		}
	}
	server := moduleFile(t, result, "hexal/server.c")
	if !strings.Contains(server, "config.hex_m_tcp_nodelay") || !strings.Contains(server, "hex_tcp_no_delay(accepted.connection") {
		t.Errorf("the configured TCP_NODELAY choice must reach every accepted connection:\n%s", server)
	}
	header := moduleFile(t, result, "modules/app.h")
	if !strings.Contains(header, "hex_http_router_route_invoke_") {
		t.Errorf("the route registration must pass the module's invoke thunk:\n%s", header)
	}
}

// Socket waits park the connection Task on the loop thread: neither the TCP
// runtime nor the server submits a worker-pool job, and the parser adapter
// makes no libuv call.
func TestHttpServerSocketWaitsUseNoWorkerPoolJob(t *testing.T) {
	source := httpServerPrelude +
		"fun serve(heap: Heap): Nil | Error do\n" +
		"    let app = App(hits = Atomic<Int32>(0))\n" +
		"    let mut router = Http.Router<App>(heap)\n" +
		"    try router.route(\"GET\", \"/\", home)\n" +
		"    let server = try Http.listen<App>(heap, Http.default_config(\"127.0.0.1\", 8080), router, @app)\n" +
		"    try server.run()\n" +
		"    server.free(heap)\n" +
		"    router.free(heap)\n" +
		"    return nil\nend\n"
	result := assertCompiles(t, source)
	for _, key := range []string{"hexal/server.c", "hexal/network.c"} {
		if strings.Contains(moduleFile(t, result, key), "hex_event_work_call") {
			t.Errorf("%s submits a worker-pool job; socket waits must park on the loop thread", key)
		}
	}
	if strings.Contains(moduleFile(t, result, "hexal/http.c"), "uv_") {
		t.Errorf("the parser adapter calls libuv")
	}
}

// The entry-module server: no source main, root-level try and defer, an
// application context reached through Ptr.
func TestHttpEntryModuleServerCompiles(t *testing.T) {
	source := "import\n    Http from std.http\nend\n\n" +
		"type App is struct\n    greeting: String<32>\nend\n\n" +
		"fun home(app: Ptr<App>, request: Http.Request, response: Http.Writer): Nil | Error do\n" +
		"    try response.content_length((^app).greeting.bytes().length())\n" +
		"    try response.write((^app).greeting.bytes())\n" +
		"    return nil\nend\n\n" +
		"let heap = Heap()\n" +
		"let app = App(greeting = \"Hello, world!\")\n" +
		"let mut router = Http.Router<App>(heap)\n" +
		"defer router.free(heap)\n" +
		"try router.route(\"GET\", \"/\", home)\n" +
		"let config = Http.default_config(\"127.0.0.1\", 8080)\n" +
		"let server = try Http.listen<App>(heap, config, router, @app)\n" +
		"defer server.free(heap)\n" +
		"try server.run()\n"
	assertCompiles(t, source)
}

func TestHttpRequestAndWriterOperationsAcceptTheApprovedSignatures(t *testing.T) {
	source := "import\n    Http from std.http\nend\n" +
		"type App is struct\n    hits: Int32\nend\n" +
		"fun inspect(app: Ptr<App>, request: Http.Request, response: Http.Writer): Nil | Error do\n" +
		"    let verb: Slice<Byte> = request.method()\n" +
		"    let target: Slice<Byte> = request.target()\n" +
		"    let path: Slice<Byte> = request.path()\n" +
		"    let headers: Slice<Http.Header> = request.headers()\n" +
		"    let token: Slice<Byte> | Nil = request.header(\"X-Token\")\n" +
		"    let heap = Heap()\n" +
		"    let body: List<Byte> = List<Byte>(heap)\n" +
		"    let step: Size | EoS | Error = request.read(body, 16)\n" +
		"    body.free(heap)\n" +
		"    try response.status(201)\n" +
		"    try response.header(\"X-Name\", verb)\n" +
		"    try response.content_length(3)\n" +
		"    try response.write(path)\n" +
		"    return nil\nend\n"
	assertCompiles(t, source)
}

func TestHttpTypeOnlyAndRegistrationDoNotSelectTheServerRuntime(t *testing.T) {
	source := httpServerPrelude +
		"fun build(h: Heap): Nil | Error do\n" +
		"    let mut router = Http.Router<App>(h)\n" +
		"    try router.route(\"GET\", \"/\", home)\n" +
		"    router.free(h)\n" +
		"    return nil\nend\n"
	result := assertCompiles(t, source)
	for _, key := range []string{"hexal/network.c", "hexal/concurrency.c", "hexal/handle.c", "hexal/event.c"} {
		if _, present := result.Files[key]; present {
			t.Errorf("route registration alone must not select %s", key)
		}
	}
	if slices.Contains(result.Dependencies, compiler.RuntimeLibuv) {
		t.Errorf("route registration alone dependencies = %v, want no libuv", result.Dependencies)
	}
}

func TestHttpRejectsInvalidServerUse(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"opaque request cannot be built", httpServerPrelude + "let request = Http.Request()\n", "Request"},
		{"write takes bytes", httpServerPrelude + "fun f(response: Http.Writer): Nil | Error do\n    try response.write(\"text\")\n    return nil\nend\n", "Slice"},
		{"read takes a list", httpServerPrelude + "fun f(request: Http.Request, bytes: Slice<Byte>): Size | EoS | Error do\n    return request.read(bytes, 4)\nend\n", "List"},
		{"status takes a number", httpServerPrelude + "fun f(response: Http.Writer): Nil | Error do\n    try response.status(\"ok\")\n    return nil\nend\n", "UInt16"},
		{"header takes a name and bytes", httpServerPrelude + "fun f(response: Http.Writer): Nil | Error do\n    try response.header(\"A\", \"b\")\n    return nil\nend\n", "Slice"},
		{"lookup takes a name", httpServerPrelude + "fun f(request: Http.Request): Slice<Byte> | Nil do\n    return request.header(1)\nend\n", "String"},
		{"stop takes nothing", httpServerPrelude + "fun f(server: Http.Server<App>) do\n    server.stop(1)\nend\n", "argument"},
		{"free takes a heap", httpServerPrelude + "fun f(server: Http.Server<App>) do\n    server.free()\nend\n", "argument"},
		{"listen needs its type argument", httpServerPrelude + "fun f(h: Heap, router: Http.Router<App>, app: Ptr<App>) do\n    let server = Http.listen(h, Http.default_config(\"127.0.0.1\", 1), router, app)\nend\n", "type argument"},
		{"listen needs a pointer context", httpServerPrelude + "fun f(h: Heap, router: Http.Router<App>, count: Int32) do\n    let server = Http.listen<App>(h, Http.default_config(\"127.0.0.1\", 1), router, count)\nend\n", "Ptr"},
		{"server and router must share the application", httpServerPrelude + "type Other is struct\n    id: Int32\nend\nfun f(h: Heap, router: Http.Router<Other>, app: Ptr<App>) do\n    let server = Http.listen<App>(h, Http.default_config(\"127.0.0.1\", 1), router, app)\nend\n", "Router"},
		{"config fields keep their types", httpServerPrelude + "fun f() do\n    let mut config = Http.default_config(\"127.0.0.1\", 1)\n    config.max_body_bytes = \"large\"\nend\n", "Size"},
		{"config is replaceable only when mutable", httpServerPrelude + "fun f() do\n    let config = Http.default_config(\"127.0.0.1\", 1)\n    config.max_body_bytes = 1\nend\n", "read-only"},
		{"a handler returns Nil | Error", httpServerPrelude + "fun bad(app: Ptr<App>, request: Http.Request, response: Http.Writer) do\nend\nfun f(h: Heap) do\n    let mut router = Http.Router<App>(h)\n    router.route(\"GET\", \"/\", bad)\nend\n", "Fun"},
		{"a handler takes the context first", httpServerPrelude + "fun bad(request: Http.Request, response: Http.Writer): Nil | Error do\n    return nil\nend\nfun f(h: Heap) do\n    let mut router = Http.Router<App>(h)\n    router.route(\"GET\", \"/\", bad)\nend\n", "Fun"},
		{"the constructor takes a heap", httpServerPrelude + "fun f() do\n    let router = Http.Router<App>()\nend\n", "argument"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) { assertRejects(t, testCase.source, testCase.want) })
	}
}

func TestMemberAccessOnAnIndexedElementParenthesizesTheDereference(t *testing.T) {
	source := "type Point is struct\n    x: Int32,\n    y: Int32\nend\n" +
		"fun first(points: Slice<Point>): Int32 do\n    return points[0].x\nend\n"
	module := rootC(t, assertCompiles(t, source))
	if !strings.Contains(module, "(*hex_slice_at_Point(") || strings.Contains(module, "return *hex_slice_at_Point(") {
		t.Fatalf("an element's member must select from a parenthesized dereference:\n%s", module)
	}
}

func TestAggregatesHoldingAnAtomicAreNotConst(t *testing.T) {
	source := "type Counter is struct\n    hits: Atomic<Int32>\nend\n" +
		"fun bump(counter: Ptr<Counter>): Int32 do\n    return (^counter).hits.fetch_add(1)\nend\n" +
		"let counter = Counter(hits = Atomic<Int32>(0))\n" +
		"print(bump(@counter))\n"
	module := withoutLineDirectives(rootC(t, assertCompiles(t, source)))
	if strings.Contains(module, "const hex_t_m3_app_Counter") || strings.Contains(module, "const hex_t_Counter") {
		t.Fatalf("a Counter holding an Atomic must not be const:\n%s", module)
	}
}
