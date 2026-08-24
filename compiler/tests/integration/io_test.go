package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// streamFacetSource is the canonical stream program: standard handles,
// memory streams over a borrowed list, transfer results, seek variants, and
// cleanup through defer.
func streamFacetSource() string {
	return "fun run(h: Heap): Nil | Error do\n" +
		"    data: List<Byte> := List<Byte>.new(h)\n" +
		"    dst: List<Byte> := List<Byte>.new(h)\n" +
		"    defer dst.free(h)\n" +
		"    defer data.free(h)\n" +
		"    mut live: Bytes := Bytes.over(data)\n" +
		"    out: IO := try IO.stdout()\n" +
		"    w: Size | Error := out.write(\"hexal\\n\".bytes())\n" +
		"    r: Size | EoS | Error := live.read(dst, 4)\n" +
		"    s: Size | Error := live.seek(Seek.Start { position = 0 })\n" +
		"    closed: Nil | Error := out.close()\n" +
		"    return nil\n" +
		"end\n" +
		"done: Nil | Error := run(Heap.new())\n"
}

func hasFile(result compiler.CompilationResult, key string) bool {
	_, ok := result.Files[key]
	return ok
}

// A program using either stream type emits the component pair; a program
// using none of IO, Bytes, or print emits none of it.
func TestStreamComponentDemand(t *testing.T) {
	streams := compileSource(streamFacetSource())
	if streams.ExitCode != compiler.ExitSuccess {
		t.Fatalf("stream facet source failed: %v", streams.Stderr)
	}
	if !hasFile(streams, "hexal/io.h") || !hasFile(streams, "hexal/io.c") {
		t.Fatalf("stream program must emit the io pair: %v", sortedKeys(streams.Files))
	}
	quiet := compileSource("value: Int32 := 1 + 1")
	if hasFile(quiet, "hexal/io.h") || hasFile(quiet, "hexal/io.c") {
		t.Fatalf("a program without IO, Bytes, or print must emit no io artifacts: %v", sortedKeys(quiet.Files))
	}
}

// The canonical stream surface satisfies the generated-C contract: component
// include order, platform confinement to io.c, one internal reserve helper,
// both transfer clamps, the single-EINTR print-sink policy, and deterministic
// artifacts.
func TestStreamGeneratedCContract(t *testing.T) {
	result := assertCompiles(t, streamFacetSource())

	if strings.Index(rootH(t, result), "\"hexal/list.h\"") > strings.Index(rootH(t, result), "\"hexal/io.h\"") {
		t.Fatalf("list.h must precede io.h in the module header:\n%s", rootH(t, result))
	}
	header := ioH(t, result)
	listAt := strings.Index(header, "\"hexal/list.h\"")
	viewAt := strings.Index(header, "\"hexal/view.h\"")
	errorAt := strings.Index(header, "\"hexal/error.h\"")
	if listAt < 0 || viewAt < 0 || errorAt < 0 || listAt > viewAt || viewAt > errorAt {
		t.Fatalf("io.h dependency order wrong:\n%s", header)
	}
	if strings.Contains(header, "#ifdef") || strings.Contains(header, "FILE") {
		t.Fatalf("io.h carries a platform branch:\n%s", header)
	}
	body := ioC(t, result)
	if !strings.Contains(body, "#ifdef _WIN32") {
		t.Fatalf("the platform dispatch belongs only to io.c:\n%s", body)
	}
	if !strings.Contains(body, "INVALID_HANDLE_VALUE") {
		t.Fatalf("an invalid Windows standard handle must fail its constructor:\n%s", body)
	}
	ioHeader := ioH(t, result)
	firstStruct := strings.Index(ioHeader, "typedef struct hex_io {")
	firstPrototype := strings.Index(ioHeader, "hex_io_open hex_io_stdin(void);")
	if firstStruct < 0 || firstPrototype < 0 || firstStruct > firstPrototype {
		t.Fatalf("io.h must declare representations before uses:\n%s", ioHeader)
	}
	if strings.Count(body, "[Runtime Error] close of a borrowed stream") != 2 {
		t.Fatalf("borrowed close must trap on both platforms:\n%s", body)
	}
	if strings.Count(body, "EINTR") != 1 {
		t.Fatalf("only the write-all sink may retry EINTR:\n%s", body)
	}
	if !strings.Contains(body, "SSIZE_MAX") || !strings.Contains(body, "0xFFFFFFFFull") {
		t.Fatalf("transfer clamps missing from io.c:\n%s", body)
	}
	readBody := body[strings.Index(body, "hex_io_transfer hex_io_read("):]
	maxZero := strings.Index(readBody, "max == 0")
	reserve := strings.Index(readBody, "hex_list_reserve_at_least_UInt8")
	if maxZero < 0 || reserve < 0 || maxZero > reserve {
		t.Fatalf("read(buffer, 0) must return before any reservation:\n%s", readBody)
	}
	if strings.Count(listH(t, result), "static inline void hex_list_reserve_at_least_UInt8") != 1 {
		t.Fatalf("the Byte list exposes exactly one reserve helper:\n%s", listH(t, result))
	}
	first := compileSource(streamFacetSource())
	second := compileSource(streamFacetSource())
	if first.ExitCode != compiler.ExitSuccess || second.ExitCode != compiler.ExitSuccess ||
		rootC(t, first) != rootC(t, second) || rootH(t, first) != rootH(t, second) {
		t.Fatalf("repeated stream compilation differs")
	}
}

// Print migrates onto the descriptor sink and drags the IO pair along.
func TestPrintSharesTheStreamBackend(t *testing.T) {
	result := assertCompiles(t, "value: Int32 := 42 print(value)")
	sink := printC(t, result)
	if strings.Contains(sink, "fwrite") {
		t.Fatalf("print still writes through C stdio:\n%s", sink)
	}
	if !strings.Contains(sink, "hex_io_write_all") || !strings.Contains(sink, "hex_io_stdout_desc") {
		t.Fatalf("print must transfer through the descriptor core:\n%s", sink)
	}
	if !hasFile(result, "hexal/io.h") || !hasFile(result, "hexal/io.c") {
		t.Fatalf("a print-only program selects the io component pair")
	}
}

// Capability checking keeps both tiers end to end.
func TestStreamCapabilityTiersEndToEnd(t *testing.T) {
	assertRejects(t,
		"fun demo(): Nil | Error do\n"+
			"    input: IO := try IO.stdin()\n"+
			"    input.write(\"x\".bytes())\n"+
			"    return nil\nend\n",
		"stream is not writable")
	source := "fun opened(): IO | Error do\n" +
		"    return IO.stdout()\n" +
		"end\n" +
		"fun demo(): Nil | Error do\n" +
		"    handle: IO := try opened()\n" +
		"    w: Size | Error := handle.write(\"x\".bytes())\n" +
		"    return nil\n" +
		"end\n" +
		"done: Nil | Error := demo()\n"
	result := assertCompiles(t, source)
	if !strings.Contains(rootH(t, result), "HEX_IO_NOT_WRITABLE") {
		t.Fatalf("unknown capability must select the runtime mask arm:\n%s", rootH(t, result))
	}
}

// One generic algorithm monomorphizes over IO and MutPtr<Bytes> with direct
// calls to each backend family and no shared dispatch.
func TestGenericStreamsMonoMorphizePerBackend(t *testing.T) {
	source := "fun drain<S>(source: S, h: Heap): Size | Error do\n" +
		"    buf: List<Byte> := List<Byte>.new(h)\n" +
		"    defer buf.free(h)\n" +
		"    n: Size | EoS | Error := source.read(buf, 64)\n" +
		"    if n is EoS then\n" +
		"        return 0\n" +
		"    end\n" +
		"    return n\n" +
		"end\n" +
		"fun run(h: Heap): Nil | Error do\n" +
		"    data: List<Byte> := List<Byte>.new(h)\n" +
		"    defer data.free(h)\n" +
		"    mut live: Bytes := Bytes.over(data)\n" +
		"    out: IO := try IO.stdout()\n" +
		"    a: Size | Error := drain<IO>(out, h)\n" +
		"    b: Size | Error := drain<MutPtr<Bytes>>(ref live, h)\n" +
		"    return nil\n" +
		"end\n" +
		"done: Nil | Error := run(Heap.new())\n"
	result := assertCompiles(t, source)
	body := rootC(t, result)
	if !strings.Contains(body, "hex_io_read_") || !strings.Contains(body, "hex_bytes_read_") {
		t.Fatalf("both backends must reach their own adapters:\n%s", body)
	}
	if strings.Contains(strings.ToLower(body), "vtable") {
		t.Fatalf("generic streams must not lower to dispatch:\n%s", body)
	}
}

// The memory backend rejects self-read and overlapping writes before any
// mutation, through integer interval arithmetic on flat addresses.
func TestMemoryBackendAliasContractsInGeneratedC(t *testing.T) {
	body := ioC(t, assertCompiles(t, streamFacetSource()))
	if !strings.Contains(body, "HEX_IO_SELF_READ") || !strings.Contains(body, "into == stream->buffer") {
		t.Fatalf("self-read must reject by List identity before reserve:\n%s", body)
	}
	if !strings.Contains(body, "HEX_IO_OVERLAP") {
		t.Fatalf("overlapping writes must reject through the transfer status:\n%s", body)
	}
	overlap := strings.Index(body, "hex_io_regions_overlap")
	if overlap < 0 || !strings.Contains(body[overlap:], "(uintptr_t)") || !strings.Contains(body[overlap:], "ckd_add") {
		t.Fatalf("overlap must be checked with uintptr_t interval arithmetic:\n%s", body)
	}
	if strings.Contains(body[overlap:], "->data <") {
		t.Fatalf("overlap must never relate unrelated C pointers")
	}
}

// Seek lowers through the Seek ADT decomposition in the module adapter.
func TestSeekLowersThroughTheADT(t *testing.T) {
	source := "fun demo(): Nil | Error do\n" +
		"    h: Heap := Heap.new()\n" +
		"    data: List<Byte> := List<Byte>.new(h)\n" +
		"    defer data.free(h)\n" +
		"    mut live: Bytes := Bytes.over(data)\n" +
		"    s: Size | Error := live.seek(Seek.Start { position = 0 })\n" +
		"    c: Size | Error := live.seek(Seek.Current { offset = 1 })\n" +
		"    e: Size | Error := live.seek(Seek.End { offset = -1 })\n" +
		"    return nil\n" +
		"end\n" +
		"done: Nil | Error := demo()\n"
	result := assertCompiles(t, source)
	header := rootH(t, result)
	for _, want := range []string{"typedef struct hex_t_Seek", "payload.hex_m_position", "payload.hex_m_offset"} {
		if !strings.Contains(header, want) {
			t.Fatalf("seek adapter missing %s:\n%s", want, header)
		}
	}
}

// The three stream type names are reserved globally; Start, Current, and End
// stay available as unqualified names.
func TestStreamNamesReservedEndToEnd(t *testing.T) {
	assertRejects(t, "type IO = { x: Int32, }", "built-in type IO cannot be redeclared")
	assertRejects(t, "type Bytes = { x: Int32, }", "built-in type Bytes cannot be redeclared")
	assertRejects(t, "type Seek as | North | South end", "built-in type Seek cannot be redeclared")
	result := assertCompiles(t, "Start: Int32 := 0 Current: Int32 := 1 End: Int32 := 2 total: Int32 := Start + Current + End")
	if !strings.Contains(rootC(t, result), "hex_v_total") {
		t.Fatalf("unqualified variant names must remain usable")
	}
}
