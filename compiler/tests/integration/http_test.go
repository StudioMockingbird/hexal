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
