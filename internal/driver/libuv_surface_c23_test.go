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
	result, err := Build(withTestBackend(t, BuildOptions{Root: dir}))
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

const fileFixtureSource = `import
    Fs from std.fs,
    Io from std.io
end

fun write_x(target: Fs.File): Size | Error do
    return target.write("x".bytes())
end

fun read_all(h: Heap, path: String): Size do
    let file = Fs.open(path, Fs.FileMode.Read())
    if file is Fs.File then
        let buffer: List<Byte> = List<Byte>(h)
        defer buffer.free(h)
        let got = file.read(buffer, 4096)
        let closed = file.close()
        if got is Size then
            return got
        end
    end
    return 0
end

fun run(h: Heap): Nil | Error do
    let out = try Fs.open("notes.txt", Fs.FileMode.Write())
    let wrote = try out.write("hello file\n".bytes())
    let empty = try out.write("".bytes())
    try out.flush()
    try out.close()
    print("wrote ", wrote, " ", empty, "\n")

    let truncated = try Fs.open("notes.txt", Fs.FileMode.Write())
    let again = try truncated.write("0123456789".bytes())
    try truncated.close()
    print("truncated ", read_all(h, "notes.txt"), "\n")

    let input = try Fs.open("notes.txt", Fs.FileMode.ReadWrite())
    let copy: Fs.File = input
    let buffer: List<Byte> = List<Byte>(h)
    defer buffer.free(h)
    let first = input.read(buffer, 4)
    let second = copy.read(buffer, 4)
    let none = input.read(buffer, 0)
    if first is Size then
        if second is Size then
            if none is Size then
                print("shared cursor ", first, " ", second, " ", none, " ", buffer.length(), " ", buffer[4], "\n")
            end
        end
    end
    let at = try input.seek(Io.Seek.Current(offset = 1))
    let tail = input.read(buffer, 100)
    let drained = input.read(buffer, 100)
    if tail is Size then
        if drained is EoS then
            print("seek ", at, " tail ", tail, " eos\n")
        end
    end
    let back = try input.seek(Io.Seek.Start(position = 0))
    let fromEnd = try input.seek(Io.Seek.End(offset = -2))
    print("positions ", back, " ", fromEnd, "\n")
    try input.close()

    let appender = try Fs.open("notes.txt", Fs.FileMode.Append())
    let appended = try appender.write("AB".bytes())
    try appender.close()
    print("appended ", read_all(h, "notes.txt"), "\n")

    let created = try Fs.open("fresh.txt", Fs.FileMode.CreateNew())
    try created.close()
    let exists = Fs.open("fresh.txt", Fs.FileMode.CreateNew())
    if exists is Error then
        print(exists.header(), ": ", exists.message, "\n")
    end
    let missing = Fs.open("absent.txt", Fs.FileMode.Read())
    if missing is Error then
        print(missing.header(), "\n")
    end
    let missingRW = Fs.open("absent.txt", Fs.FileMode.ReadWrite())
    if missingRW is Error then
        print(missingRW.header(), "\n")
    end
    let nul = Fs.open("bad\0name.txt", Fs.FileMode.Write())
    if nul is Error then
        print(nul.header(), ": ", nul.message, "\n")
    end
    let reader = try Fs.open("notes.txt", Fs.FileMode.Read())
    let denied = write_x(reader)
    if denied is Error then
        print(denied.header(), ": ", denied.message, "\n")
    end
    try reader.close()
    return nil
end

let r: Nil | Error = run(Heap())
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
	"permission denied: file is not writable\n"

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
    let task = try spawn spawned()
    return task.join()
end
let j: Int32 | Error = join_one()
`
	runRuntimeFixture(t, runtimeFixture{name: "file-task", source: source, stdout: fileFixtureStdout, exitZero: true})
}

func TestRuntimeTimeAndSleep(t *testing.T) {
	requireBackend(t)
	source := `import
    Time from std.time
end

fun sleeper(ms: UInt64): Int32 do
    let start = Time.now()
    Time.sleep(Time.milliseconds(ms))
    if start.elapsed() >= Time.milliseconds(ms) then
        return 1
    end
    return 0
end

fun busy(): Int32 do
    let mut total: Int32 = 0
    let mut i: Int32 = 0
    while i < 1000 do
        total = total + 1
        i = i + 1
    end
    return total
end

fun run(): Int32 | Error do
    let a = try spawn sleeper(30)
    let b = try spawn sleeper(1)
    let c = try spawn busy()
    let worked = c.join()
    return a.join() + b.join() + worked
end

let zero = Time.now()
Time.sleep(Time.nanoseconds(0))
let tiny = Time.now()
Time.sleep(Time.nanoseconds(1))
print(tiny.elapsed() >= Time.nanoseconds(1), "\n")
let d = Time.seconds(2) + Time.milliseconds(500)
print(d.as_seconds(), " ", d.as_milliseconds(), " ", (d - Time.seconds(1)).as_microseconds(), "\n")
print(Time.nanoseconds(18446744073709551615).as_nanoseconds(), "\n")
let result = run()
if result is Int32 then
    print(result, "\n")
end
`
	runRuntimeFixture(t, runtimeFixture{name: "time", source: source, stdout: "true\n2 2500 1500000\n18446744073709551615\n1002\n", exitZero: true})
}

func TestRuntimeTimeTraps(t *testing.T) {
	requireBackend(t)
	for _, fixture := range []runtimeFixture{
		{name: "construct-overflow", source: "import\n  Time from std.time\nend\nprint(Time.seconds(18446744074).as_seconds())\n", stderr: "[Runtime Error] duration overflow\n"},
		{name: "add-overflow", source: "import\n  Time from std.time\nend\nprint((Time.nanoseconds(18446744073709551615) + Time.nanoseconds(1)).as_seconds())\n", stderr: "[Runtime Error] duration overflow\n"},
		{name: "underflow", source: "import\n  Time from std.time\nend\nprint((Time.seconds(1) - Time.seconds(2)).as_seconds())\n", stderr: "[Runtime Error] duration underflow\n"},
		{name: "sleep-too-large", source: "import\n  Time from std.time\nend\nTime.sleep(Time.nanoseconds(9223372036854775808))\nprint(\"unreachable\")\n", stderr: "[Runtime Error] sleep duration too large\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			runRuntimeFixture(t, fixture)
		})
	}
}
