package compiler

import (
	"strings"
	"testing"
)

// RFC 0040 integration tests: File.open, FileMode, the File method surface,
// and the Stdio intrinsic.

func TestFileOpenAndWriteCompile(t *testing.T) {
	source := "fun write_report(h: Heap, path: String, report: String): Nil | Error\n    file: File = try File.open(path, FileMode.Write)\n    defer file.close()\n    result: Nil | Error = try file.write_text(report)\n    flushed: Nil | Error = try file.flush()\n    return nil\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{
		"sw_file_open(",
		"SW_FILE_WRITE",
		"sw_file_write_text(",
		"sw_file_flush(",
		"sw_file_close(",
	} {
		if !strings.Contains(result.MainC, fragment) && !strings.Contains(result.MainH, fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
}

func TestFileReadTextAndBytesCompile(t *testing.T) {
	source := "fun read_text(h: Heap, path: String): String | Error\n    file: File = try File.open(path, FileMode.Read)\n    defer file.close()\n    text: String = try file.read_text(h)\n    return text\nend\nfun read_bytes(h: Heap, path: String): List<Byte> | Error\n    file: File = try File.open(path, FileMode.Read)\n    defer file.close()\n    bytes: List<Byte> = try file.read_bytes(h)\n    return bytes\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{"sw_file_read_text(", "sw_file_read_bytes_UInt8(", "sw_list_push_UInt8("} {
		if !strings.Contains(result.MainC, fragment) && !strings.Contains(result.MainH, fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
}

func TestFileAppendAndWriteViewCompile(t *testing.T) {
	source := "fun append_packet(h: Heap, path: String, packet: View<Byte>): Nil | Error\n    file: File = try File.open(path, FileMode.Append)\n    defer file.close()\n    written: Nil | Error = try file.write(packet)\n    return nil\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "SW_FILE_APPEND") || !strings.Contains(result.MainC, "sw_file_write_bytes(") {
		t.Fatalf("generated C lacks the append surface:\n%s", result.MainC)
	}
}

func TestFileModeValuesCompileAndCompare(t *testing.T) {
	source := "fun modes(): Bool\n    a: FileMode = FileMode.Read\n    b: FileMode = FileMode.Write\n    c: FileMode = FileMode.Append\n    return a != b and a == FileMode.Read\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{"SW_FILE_READ", "SW_FILE_WRITE", "SW_FILE_APPEND", "sw_equal_sw_file_mode"} {
		if !strings.Contains(result.MainC, fragment) && !strings.Contains(result.MainH, fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
}

func TestStdioCompiles(t *testing.T) {
	source := "fun greet(): Nil | Error\n    ready: Nil | Error = try Stdio.stdout().write_text(\"ready\\n\")\n    warning: Nil | Error = try Stdio.stderr().write_text(\"warning\\n\")\n    flushed: Nil | Error = try Stdio.stdout().flush()\n    return nil\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{"(sw_file){ stdout, SW_FILE_WRITE, false }", "(sw_file){ stderr, SW_FILE_WRITE, false }"} {
		if !strings.Contains(result.MainC, fragment) {
			t.Fatalf("generated C lacks %s:\n%s", fragment, result.MainC)
		}
	}
}

func TestFileOpenRejectsLiteralPaths(t *testing.T) {
	for _, source := range []string{
		"fun f(): File | Error\n    file: File = try File.open(\"\", FileMode.Read)\n    return file\nend\n",
		"fun f(): File | Error\n    file: File = try File.open(\"bad\\u{0}path\", FileMode.Read)\n    return file\nend\n",
		"fun f(): File | Error\n    file: File = try File.open(\"caf\\u{00E9}.txt\", FileMode.Read)\n    return file\nend\n",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want literal path diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

func TestStdioCompileTimeRejections(t *testing.T) {
	for _, source := range []string{
		"fun f(): Nil | Error\n    result: Nil | Error = try Stdio.stdin().write_text(\"x\")\n    return nil\nend\n",
		"fun f(): Nil | Error\n    result: Nil | Error = try Stdio.stdin().flush()\n    return nil\nend\n",
		"fun f(): Nil\n    Stdio.stdout().close()\nend\n",
		"fun f(h: Heap): Nil | Error\n    result: Nil | Error = try Stdio.stdin().read_bytes(h)\n    return nil\nend\n",
		"fun f(h: Heap): Nil | Error\n    bytes: Array<UInt8, 1> = [1]\n    view: View<UInt8> = bytes.slice(0, 1)\n    result: Nil | Error = try Stdio.stdout().write(view)\n    return nil\nend\n",
		"fun f(h: Heap): Nil | Error\n    text: String = try Stdio.stdout().read_text(h)\n    return text\nend\n",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want Stdio rejection, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

func TestFileMethodsRejectWrongArguments(t *testing.T) {
	source := "fun f(h: Heap, path: String): Nil | Error\n    file: File = try File.open(path, FileMode.Write)\n    defer file.close()\n    result: Nil | Error = try file.write_text(42)\n    return nil\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "write_text requires String") {
		t.Fatalf("want write_text diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestFileValuesAreNotEqualityComparable(t *testing.T) {
	source := "fun f(h: Heap, path: String): Bool | Error\n    first: File = try File.open(path, FileMode.Read)\n    second: File = first\n    return first == second\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "File values are not equality-comparable") {
		t.Fatalf("want File equality diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestFileTypesAreProtected(t *testing.T) {
	for _, source := range []string{
		"type File = { x: Int32, }\n",
		"type FileMode = { x: Int32, }\n",
		"type Stdio = { x: Int32, }\n",
		"fun Stdio(): Int32\n    return 0\nend\n",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want protected-name diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}
