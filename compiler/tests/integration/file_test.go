package integration

import (
	"slices"
	"strings"
	"testing"
)

// The regular-file surface: File.open with the five FileMode variants and the
// read, write, seek, flush, and close operations over libuv.

func fileFacetSource() string {
	return "import\n    Fs from \"std/fs\",\n    Io from \"std/io\"\nend\nfun run(h: Heap): Nil | Error do\n" +
		"    out := try Fs.open(\"notes.txt\", Fs.FileMode.Write())\n" +
		"    defer out.close()\n" +
		"    wrote := try out.write(\"hexal\\n\".bytes())\n" +
		"    try out.flush()\n" +
		"    input := try Fs.open(\"notes.txt\", Fs.FileMode.ReadWrite())\n" +
		"    buffer: List<Byte> := List<Byte>(h)\n" +
		"    defer buffer.free(h)\n" +
		"    got: Size | EoS | Error := input.read(buffer, 4096)\n" +
		"    at := try input.seek(Io.Seek.End(offset = 0))\n" +
		"    try input.close()\n" +
		"    result := Fs.open(\"settings.toml\", Fs.FileMode.Read())\n" +
		"    if result is Error then\n" +
		"        if result.kind == ErrorKind.NotFound() then\n" +
		"            return nil\n" +
		"        end\n" +
		"    end\n" +
		"    return nil\n" +
		"end\n" +
		"done: Nil | Error := run(Heap())\n"
}

func TestFileSurfaceAcceptsSettledOperations(t *testing.T) {
	assertCompiles(t, fileFacetSource())
	for _, mode := range []string{"Read", "Write", "Append", "ReadWrite", "CreateNew"} {
		assertCompiles(t, "import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode."+mode+"())\n    try x.close()\n    return nil\nend\n")
	}
	// A File crosses a Task boundary like IO.
	assertCompiles(t, "import\n    Fs from \"std/fs\"\nend\nfun g(f: Fs.File): Int32 do\n    return 1\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    t := try spawn g(x)\n    return nil\nend\n")
}

// A File's generation-checked handle makes closing safe from any copy, so it
// occupies every ordinary complete-value position, not only the ephemeral
// ones a raw shallow-copy value like IO is restricted to.
func TestFileOccupiesEveryCommonHandleStoragePosition(t *testing.T) {
	for _, source := range []string{
		// Struct member (ObjectMember).
		"import\n    Fs from \"std/fs\"\nend\ntype Holder is struct f: Fs.File end\nfun make(h: Heap): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    holder := Holder(f = x)\n    try holder.f.close()\n    return nil\nend",
		// ADT payload.
		"import\n    Fs from \"std/fs\"\nend\ntype Wrapped is union | Present as f: Fs.File end | Absent end\nfun make(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    w := Wrapped.Present(f = x)\n    return nil\nend",
		// Array element.
		"import\n    Fs from \"std/fs\"\nend\nfun make(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    files: Array<Fs.File, 1> := [x]\n    try files[0].close()\n    return nil\nend",
		// List element and Dict value.
		"import\n    Fs from \"std/fs\"\nend\nfun make(h: Heap): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    files: List<Fs.File> := List<Fs.File>(h)\n    defer files.free(h)\n    files.push(x)\n    try files[0].close()\n    return nil\nend",
		"import\n    Fs from \"std/fs\"\nend\nfun make(h: Heap): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    files: Dict<Int32, Fs.File> := Dict<Int32, Fs.File>(h)\n    defer files.free(h)\n    files.insert(1, x)\n    try files.get(1).close()\n    return nil\nend",
	} {
		assertCompiles(t, source)
	}
}

func TestFileSurfaceRejectsUnlistedOperations(t *testing.T) {
	// The former File and FileMode names are ordinary user-declarable names
	// now; the std/fs alias is the only route to the canonical types.
	assertCompiles(t, "type File is UInt64")
	assertCompiles(t, "type FileMode is UInt64")
	for _, testCase := range []struct{ source, want string }{
		{"import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(5, Fs.FileMode.Write())\n    return nil\nend", "expected String"},
		{"import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.create(\"a\")\n    return nil\nend", "declaration create is private to module std/fs"},
		{"import\n    Fs from \"std/fs\"\nend\nfun f(h: Heap): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    b: List<Byte> := List<Byte>(h)\n    r := x.read(b, 1)\n    return nil\nend", "stream is not readable"},
		{"import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Read())\n    r := x.write(\"x\".bytes())\n    return nil\nend", "stream is not writable"},
		{"import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Read())\n    r := x.flush()\n    return nil\nend", "stream is not writable"},
		{"import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Read())\n    try x.close()\n    try x.close()\n    return nil\nend", "this stream was closed on every path"},
		{"import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    defer x.flush()\n    return nil\nend", "only File.close() may be deferred"},
	} {
		assertRejects(t, testCase.source, testCase.want)
	}
	for _, operation := range []string{"stat", "rename", "remove", "sendfile", "metadata"} {
		assertRejects(t, "import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Write())\n    r := x."+operation+"()\n    return nil\nend", "File has no method "+operation)
	}
}

// File alone selects libuv, mimalloc, and the native bootstrap but neither
// scheduler nor event bridge; File with Task selects the event bridge too.
func TestFileComponentDemand(t *testing.T) {
	alone := assertCompiles(t, "import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Read())\n    try x.close()\n    return nil\nend\nr: Nil | Error := f()\n")
	if !hasFile(alone, "hexal/file.h") || !hasFile(alone, "hexal/file.c") {
		t.Fatalf("File program must emit the file pair: %v", sortedKeys(alone.Files))
	}
	if !slices.Equal(dependencyNames(alone), []string{"libuv", "mimalloc"}) {
		t.Fatalf("File must select libuv and mimalloc; got %v", dependencyNames(alone))
	}
	if hasFile(alone, "hexal/concurrency.c") || hasFile(alone, "hexal/event.c") || hasFile(alone, "hexal/io.c") {
		t.Fatalf("File alone must select no scheduler, event bridge, or IO pair: %v", sortedKeys(alone.Files))
	}
	if strings.Contains(alone.Files["hexal/file.c"], "hex_task_current") || strings.Contains(alone.Files["hexal/file.c"], "hexal/event.h") {
		t.Fatalf("File without Task must emit only the synchronous path:\n%s", alone.Files["hexal/file.c"])
	}
	main := rootC(t, alone)
	if !strings.Contains(main, "int main(void) {\n    hex_runtime_native_init();\n") || strings.Contains(main, "hex_scheduler_init") {
		t.Fatalf("File root must bootstrap libuv first and start no scheduler:\n%s", main)
	}

	tasked := assertCompiles(t, "import\n    Fs from \"std/fs\"\nend\nfun g(): Int32 | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Read())\n    try x.close()\n    return 1\nend\nfun f(): Int32 | Error do\n    t := try spawn g()\n    return t.join()\nend\nr: Int32 | Error := f()\n")
	event := tasked.Files["hexal/event.c"]
	if !strings.Contains(event, "void hex_event_submit(hex_task *task, hex_event_command *command) {") || !strings.Contains(tasked.Files["hexal/event.h"], "void *hex_event_loop_handle(void);") {
		t.Fatalf("File with Task must expose the component command API:\n%s", event)
	}
	if strings.Count(event, "typedef struct hex_event_command {") != 0 || strings.Contains(tasked.Files["hexal/event.h"], "uv_") {
		t.Fatalf("the command typedef lives once, in event.h, with no libuv name:\n%s\n%s", tasked.Files["hexal/event.h"], event)
	}
	if !strings.Contains(tasked.Files["hexal/file.c"], "hex_event_submit(task, &request->command);") {
		t.Fatalf("File with Task must emit the Task-parking path:\n%s", tasked.Files["hexal/file.c"])
	}

	// Current IO and print without Task keep their direct path.
	io := assertCompiles(t, streamFacetSource())
	if slices.Contains(dependencyNames(io), "libuv") || hasFile(io, "hexal/file.c") {
		t.Fatalf("IO alone must select no libuv and no File artifacts: %v %v", dependencyNames(io), sortedKeys(io.Files))
	}
}

func TestFileGeneratedCContract(t *testing.T) {
	result := assertCompiles(t, fileFacetSource())
	header := result.Files["hexal/file.h"]
	if strings.Contains(header, "uv_") || strings.Contains(header, "uv.h") || strings.Contains(header, "#ifdef") || strings.Contains(header, "HANDLE") {
		t.Fatalf("file.h must expose no libuv, platform, or conditional name:\n%s", header)
	}
	if !strings.Contains(header, "hex_handle handle;") || !strings.Contains(header, "uint8_t access;") || strings.Contains(header, "owned") {
		t.Fatalf("File lowers to a generation-checked handle and access mask only:\n%s", header)
	}
	source := result.Files["hexal/file.c"]
	for _, required := range []string{
		"UV_FS_O_RDONLY,",
		"UV_FS_O_WRONLY | UV_FS_O_CREAT | UV_FS_O_TRUNC,",
		"UV_FS_O_WRONLY | UV_FS_O_CREAT | UV_FS_O_APPEND,",
		"UV_FS_O_RDWR,",
		"UV_FS_O_WRONLY | UV_FS_O_CREAT | UV_FS_O_EXCL,",
		"uv_fs_open(loop, &request->fs, request->path, request->flags, 0666, done)",
		"uv_fs_read(loop, &request->fs, request->desc, &request->buffer, 1, -1, done)",
		"uv_fs_write(loop, &request->fs, request->desc, &request->buffer, 1, -1, done)",
		"uv_fs_fsync(loop, &request->fs, request->desc, done)",
		"uv_fs_close(loop, &request->fs, request->desc, done)",
		"uv_translate_sys_error((int)GetLastError())",
		"uv_translate_sys_error(errno)",
		"return (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidPath};",
		"return (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput};",
		"hex_handle_error_kind(status, &mapped)",
		"hex_handle_resolve(file.handle, HEX_HANDLE_KIND_FILE)",
		"hex_handle_close_begin(file.handle, HEX_HANDLE_KIND_FILE)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("file.c lacks %q:\n%s", required, source)
		}
	}
	// File-specific categories and contextual overrides stay in file.c.
	for _, kind := range []string{"PermissionDenied", "InvalidPath", "NotADirectory", "IsADirectory", "DirectoryNotEmpty", "ReadOnly", "Busy", "Closed"} {
		if !strings.Contains(source, "hex_tag_ErrorKind_"+kind) {
			t.Fatalf("file.c lacks portable classification %q", kind)
		}
	}
	// Shared libuv conditions classify through the common handle mapper, not
	// a second switch duplicated into File.
	handle := result.Files["hexal/handle.c"]
	for _, kind := range []string{"NotFound", "PermissionDenied", "AlreadyExists", "InvalidInput", "ResourceExhausted", "Unsupported", "Cancelled", "Interrupted", "TimedOut", "AddressInUse", "AddressUnavailable", "ConnectionRefused", "ConnectionReset", "ConnectionAborted", "HostUnreachable", "NetworkUnreachable", "BrokenPipe", "NotConnected"} {
		if !strings.Contains(handle, "hex_tag_ErrorKind_"+kind) {
			t.Fatalf("handle.c lacks common portable classification %q", kind)
		}
	}
	for _, kind := range []string{"NotFound", "AlreadyExists", "Interrupted", "Cancelled", "Unsupported"} {
		if strings.Contains(source, "hex_tag_ErrorKind_"+kind) {
			t.Fatalf("file.c must not duplicate the common classification %q", kind)
		}
	}
	if !strings.Contains(source, "\"filesystem error\"") {
		t.Fatalf("file.c lacks its Other fallback header:\n%s", source)
	}
	if strings.Count(source, "uv_fs_req_cleanup(") != 1 || strings.Contains(source, "uv_queue_work") || strings.Contains(source, "errno=") || strings.Contains(source, "winerr=") {
		t.Fatalf("file.c must clean each request in one place, never use the work pool, and expose no native code:\n%s", source)
	}
	// The embedded-NUL check precedes any request.
	open := source[strings.Index(source, "hex_file_opened hex_file_open("):]
	if strings.Index(open, "memchr(path->data, 0, path->byte_length)") > strings.Index(open, "hex_file_run(") {
		t.Fatalf("File.open must reject an embedded NUL before submitting a request:\n%s", open)
	}
	// Capability checks precede the zero-length fast path.
	read := source[strings.Index(source, "hex_file_transfer hex_file_read("):]
	if strings.Index(read, "HEX_FILE_ACCESS_READ") > strings.Index(read, "if (max == 0)") {
		t.Fatalf("read must check capability before the zero-length path:\n%s", read)
	}
	adapters := rootH(t, result)
	for _, required := range []string{"hex_file_open_", "hex_file_read_", "hex_file_write_", "hex_file_seek_", "hex_file_flush_", "hex_file_close_"} {
		if !strings.Contains(adapters, "static inline") || !strings.Contains(adapters, required) {
			t.Fatalf("module header lacks adapter %s:\n%s", required, adapters)
		}
	}
	if !strings.Contains(adapters, "\"hexal/file.h\"") || !strings.Contains(adapters, "\"hexal/seek.h\"") {
		t.Fatalf("module header must include the file and seek components:\n%s", adapters)
	}
}

// A component .c that includes hexal/list.h directly (file.c, io.c) must see
// the Task typedef a List<Task<...>> element spells.
func TestListOfTasksHeaderIncludesConcurrency(t *testing.T) {
	result := assertCompiles(t, "import\n    Fs from \"std/fs\"\nend\nfun work(): Int32 do\n    return 1\nend\n"+
		"fun run(h: Heap): Int32 | Error do\n"+
		"    tasks: List<Task<Int32>> := List<Task<Int32>>(h)\n"+
		"    defer tasks.free(h)\n"+
		"    tasks.push(try spawn work())\n"+
		"    out := try Fs.open(\"a\", Fs.FileMode.Write())\n"+
		"    try out.close()\n"+
		"    return 0\n"+
		"end\n")
	list := result.Files["hexal/list.h"]
	if !strings.Contains(list, "#include \"hexal/concurrency.h\"") {
		t.Fatalf("list.h names a Task element without including the concurrency component:\n%s", list)
	}
	quiet := assertCompiles(t, "fun run(h: Heap) do\n    values: List<Int32> := List<Int32>(h)\n    defer values.free(h)\nend\n")
	if strings.Contains(quiet.Files["hexal/list.h"], "concurrency.h") {
		t.Fatalf("list.h without a handle element must not include the concurrency component")
	}
}
