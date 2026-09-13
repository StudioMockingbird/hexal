//go:build c23

package driver

// Runtime fixtures for the libuv-backed File and time surfaces, built through
// the real driver and run as executables. Each fixture asserts exact stdout,
// stderr, and exit status.

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type runtimeFixture struct {
	name     string
	source   string
	stdout   string
	stderr   string
	exitZero bool
}

func runRuntimeFixture(t *testing.T, fixture runtimeFixture) {
	t.Helper()
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", fixture.source)
	result, err := Build(BuildOptions{Root: dir})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	command := exec.Command(result.Executable)
	command.Dir = dir
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		t.Fatalf("running %s failed: %v", result.Executable, runErr)
	}
	if (runErr == nil) != fixture.exitZero {
		t.Fatalf("exit zero = %t, want %t; stdout=%q stderr=%q", runErr == nil, fixture.exitZero, stdout.String(), stderr.String())
	}
	if got := strings.ReplaceAll(stdout.String(), "\r\n", "\n"); got != fixture.stdout {
		t.Fatalf("stdout = %q, want %q", got, fixture.stdout)
	}
	if got := strings.ReplaceAll(stderr.String(), "\r\n", "\n"); got != fixture.stderr {
		t.Fatalf("stderr = %q, want %q", got, fixture.stderr)
	}
}

const fileFixtureSource = `fun write_x(target: File): Size | Error do
    return target.write("x".bytes())
end

fun read_all(h: Heap, path: String): Size do
    file := File.open(path, FileMode.Read())
    if file is File then
        buffer: List<Byte> := List<Byte>(h)
        defer buffer.free(h)
        got := file.read(buffer, 4096)
        closed := file.close()
        if got is Size then
            return got
        end
    end
    return 0
end

fun run(h: Heap): Nil | Error do
    out := try File.open("notes.txt", FileMode.Write())
    wrote := try out.write("hello file\n".bytes())
    empty := try out.write("".bytes())
    try out.flush()
    try out.close()
    print("wrote ", wrote, " ", empty, "\n")

    truncated := try File.open("notes.txt", FileMode.Write())
    again := try truncated.write("0123456789".bytes())
    try truncated.close()
    print("truncated ", read_all(h, "notes.txt"), "\n")

    input := try File.open("notes.txt", FileMode.ReadWrite())
    copy: File := input
    buffer: List<Byte> := List<Byte>(h)
    defer buffer.free(h)
    first := input.read(buffer, 4)
    second := copy.read(buffer, 4)
    none := input.read(buffer, 0)
    if first is Size then
        if second is Size then
            if none is Size then
                print("shared cursor ", first, " ", second, " ", none, " ", buffer.length(), " ", buffer[4], "\n")
            end
        end
    end
    at := try input.seek(Seek.Current(offset = 1))
    tail := input.read(buffer, 100)
    drained := input.read(buffer, 100)
    if tail is Size then
        if drained is EoS then
            print("seek ", at, " tail ", tail, " eos\n")
        end
    end
    back := try input.seek(Seek.Start(position = 0))
    fromEnd := try input.seek(Seek.End(offset = -2))
    print("positions ", back, " ", fromEnd, "\n")
    try input.close()

    appender := try File.open("notes.txt", FileMode.Append())
    appended := try appender.write("AB".bytes())
    try appender.close()
    print("appended ", read_all(h, "notes.txt"), "\n")

    created := try File.open("fresh.txt", FileMode.CreateNew())
    try created.close()
    exists := File.open("fresh.txt", FileMode.CreateNew())
    if exists is Error then
        print(exists.header, ": ", exists.message, "\n")
    end
    missing := File.open("absent.txt", FileMode.Read())
    if missing is Error then
        print(missing.header, "\n")
    end
    missingRW := File.open("absent.txt", FileMode.ReadWrite())
    if missingRW is Error then
        print(missingRW.header, "\n")
    end
    nul := File.open("bad\0name.txt", FileMode.Write())
    if nul is Error then
        print(nul.header, ": ", nul.message, "\n")
    end
    reader := try File.open("notes.txt", FileMode.Read())
    denied := write_x(reader)
    if denied is Error then
        print(denied.header, ": ", denied.message, "\n")
    end
    try reader.close()
    return nil
end

r: Nil | Error := run(Heap())
if r != nil then
    print("unexpected failure\n")
end
`

const fileFixtureStdout = "wrote 11 0\n" +
	"truncated 10\n" +
	"shared cursor 4 4 0 8 52\n" +
	"seek 9 tail 1 eos\n" +
	"positions 0 8\n" +
	"appended 12\n" +
	"already exists: file open failed\n" +
	"not found\n" +
	"not found\n" +
	"invalid path: file open failed\n" +
	"filesystem error: file is not writable\n"

func TestRuntimeFileSynchronousPath(t *testing.T) {
	requireBackend(t)
	runRuntimeFixture(t, runtimeFixture{name: "file-sync", source: fileFixtureSource, stdout: fileFixtureStdout, exitZero: true})
}

// The same File program with a Task selects the Task-parking path: calls
// from the root Task and from a spawned Task park on the event bridge.
func TestRuntimeFileTaskPath(t *testing.T) {
	requireBackend(t)
	source := fileFixtureSource + `
fun spawned(): Int32 | Error do
    return 7
end

fun join_one(): Int32 | Error do
    task := try spawn spawned()
    return task.join()
end
j: Int32 | Error := join_one()
`
	runRuntimeFixture(t, runtimeFixture{name: "file-task", source: source, stdout: fileFixtureStdout, exitZero: true})
}

func TestRuntimeTimeAndSleep(t *testing.T) {
	requireBackend(t)
	source := `fun sleeper(ms: UInt64): Int32 do
    start := Instant.now()
    Task.sleep(Duration.milliseconds(ms))
    if start.elapsed() >= Duration.milliseconds(ms) then
        return 1
    end
    return 0
end

fun busy(): Int32 do
    mut total: Int32 := 0
    mut i: Int32 := 0
    while i < 1000 do
        total = total + 1
        i = i + 1
    end
    return total
end

fun run(): Int32 | Error do
    a := try spawn sleeper(30)
    b := try spawn sleeper(1)
    c := try spawn busy()
    worked := c.join()
    return a.join() + b.join() + worked
end

zero := Instant.now()
Task.sleep(Duration.nanoseconds(0))
tiny := Instant.now()
Task.sleep(Duration.nanoseconds(1))
print(tiny.elapsed() >= Duration.nanoseconds(1), "\n")
d := Duration.seconds(2) + Duration.milliseconds(500)
print(d.as_seconds(), " ", d.as_milliseconds(), " ", (d - Duration.seconds(1)).as_microseconds(), "\n")
print(Duration.nanoseconds(18446744073709551615).as_nanoseconds(), "\n")
result := run()
if result is Int32 then
    print(result, "\n")
end
`
	runRuntimeFixture(t, runtimeFixture{name: "time", source: source, stdout: "true\n2 2500 1500000\n18446744073709551615\n1002\n", exitZero: true})
}

func TestRuntimeTimeTraps(t *testing.T) {
	requireBackend(t)
	for _, fixture := range []runtimeFixture{
		{name: "construct-overflow", source: "print(Duration.seconds(18446744074).as_seconds())\n", stderr: "[Runtime Error] duration overflow\n"},
		{name: "add-overflow", source: "print((Duration.nanoseconds(18446744073709551615) + Duration.nanoseconds(1)).as_seconds())\n", stderr: "[Runtime Error] duration overflow\n"},
		{name: "underflow", source: "print((Duration.seconds(1) - Duration.seconds(2)).as_seconds())\n", stderr: "[Runtime Error] duration underflow\n"},
		{name: "sleep-too-large", source: "Task.sleep(Duration.nanoseconds(9223372036854775808))\nprint(\"unreachable\")\n", stderr: "[Runtime Error] sleep duration too large\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			runRuntimeFixture(t, fixture)
		})
	}
}
