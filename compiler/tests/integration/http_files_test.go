package integration

import (
	"slices"
	"strings"
	"testing"

	"hexal/compiler"
)

const httpFilesPrelude = "import\n    Http from std.http\nend\n" +
	"type App is struct\n    hits: Int32\nend\n" +
	"fun home(app: Ptr<App>, request: Http.Request, response: Http.Writer): Nil | Error do\n    return nil\nend\n"

func TestHttpFileServerSurfaceCompilesAndSelectsOnlyBufferedTransfer(t *testing.T) {
	source := httpFilesPrelude +
		"fun serve(app: Ptr<App>, request: Http.Request, response: Http.Writer): Nil | Error do\n" +
		"    let heap = Heap()\n" +
		"    let files: Http.FileServer = try Http.FileServer(heap, \"public\", \"index.html\", false, \"max-age=60\")\n" +
		"    try files.serve_file(request, response, \"a.txt\")\n" +
		"    try files.serve_directory(request, response, \"\")\n" +
		"    files.free(heap)\n" +
		"    return nil\nend\n" +
		"fun mount(heap: Heap): Nil | Error do\n" +
		"    let files = try Http.FileServer(heap, \"public\", \"index.html\", true, \"\")\n" +
		"    let mut router = Http.Router<App>(heap)\n" +
		"    try router.mount(\"/assets\", files)\n" +
		"    router.free(heap)\n" +
		"    files.free(heap)\n" +
		"    return nil\nend\n"
	result := assertCompiles(t, source)
	for _, key := range []string{"hexal/fileserver.c", "hexal/fileserver.h", "hexal/server.c", "hexal/event.c"} {
		if _, present := result.Files[key]; !present {
			t.Errorf("file serving does not select %s", key)
		}
	}
	if !slices.Contains(result.Dependencies, compiler.RuntimeLibuv) {
		t.Errorf("file serving dependencies = %v, want libuv", result.Dependencies)
	}
	// Transfer is a bounded buffered copy through the writer: no zero-copy
	// call reaches libuv-owned sockets from the file runtime.
	files := moduleFile(t, result, "hexal/fileserver.c")
	for _, forbidden := range []string{"sendfile", "TransmitFile", "uv_fs_sendfile", "mmap", "realpath"} {
		if strings.Contains(files, forbidden) {
			t.Errorf("fileserver.c mentions %s; transfer is buffered and containment is handle-relative", forbidden)
		}
	}
	for name, content := range result.Files {
		if strings.HasSuffix(name, ".c") && strings.Contains(content, "TransmitFile") {
			t.Errorf("%s selects TransmitFile", name)
		}
	}
}

func TestHttpServerWithoutFileServerSelectsNoFileComponent(t *testing.T) {
	source := httpFilesPrelude +
		"fun serve(heap: Heap): Nil | Error do\n" +
		"    let mut router = Http.Router<App>(heap)\n" +
		"    try router.route(\"GET\", \"/\", home)\n" +
		"    let app = App(hits = 0)\n" +
		"    let server = try Http.listen<App>(heap, Http.default_config(\"127.0.0.1\", 8080), router, @app)\n" +
		"    server.stop()\n" +
		"    server.free(heap)\n" +
		"    router.free(heap)\n" +
		"    return nil\nend\n"
	result := assertCompiles(t, source)
	for _, key := range []string{"hexal/fileserver.c", "hexal/fileserver.h"} {
		if _, present := result.Files[key]; present {
			t.Errorf("a server without a file server selected %s", key)
		}
	}
	server := moduleFile(t, result, "hexal/server.c")
	for _, absent := range []string{"hex_http_router_mount_raw", "hex_http_find_mount", "hexal/fileserver.h"} {
		if strings.Contains(server, absent) {
			t.Errorf("server.c carries the mount table (%s) without any mount use", absent)
		}
	}
}

func TestHttpFileServerRejectsInvalidUse(t *testing.T) {
	serving := httpFilesPrelude + "fun f(app: Ptr<App>, request: Http.Request, response: Http.Writer, files: Http.FileServer): Nil | Error do\n"
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"opaque file server cannot be built", httpFilesPrelude + "let files = Http.FileServer()\n", "argument"},
		{"root is a string", httpFilesPrelude + "fun f(h: Heap) do\n    let files = Http.FileServer(h, 1, \"index.html\", false, \"\")\nend\n", "String"},
		{"dotfiles is a bool", httpFilesPrelude + "fun f(h: Heap) do\n    let files = Http.FileServer(h, \"public\", \"index.html\", \"no\", \"\")\nend\n", "Bool"},
		{"cache control is a string", httpFilesPrelude + "fun f(h: Heap) do\n    let files = Http.FileServer(h, \"public\", \"index.html\", false, 60)\nend\n", "String"},
		{"constructor takes five arguments", httpFilesPrelude + "fun f(h: Heap) do\n    let files = Http.FileServer(h, \"public\", \"index.html\", false)\nend\n", "argument"},
		{"serve_file takes the request and writer", serving + "    try files.serve_file(response, request, \"a\")\n    return nil\nend\n", "Request"},
		{"serve_file takes a string path", serving + "    try files.serve_file(request, response, 1)\n    return nil\nend\n", "String"},
		{"serve_directory takes a string path", serving + "    try files.serve_directory(request, response, 1)\n    return nil\nend\n", "String"},
		{"free takes a heap", httpFilesPrelude + "fun f(files: Http.FileServer) do\n    files.free()\nend\n", "argument"},
		{"mount takes a file server", httpFilesPrelude + "fun f(h: Heap) do\n    let mut router = Http.Router<App>(h)\n    router.mount(\"/a\", 3)\nend\n", "FileServer"},
		{"mount takes a string prefix", httpFilesPrelude + "fun f(h: Heap, files: Http.FileServer) do\n    let mut router = Http.Router<App>(h)\n    router.mount(1, files)\nend\n", "String"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) { assertRejects(t, testCase.source, testCase.want) })
	}
}
