package integration

import (
	"slices"
	"strings"
	"testing"

	"hexal/compiler"
)

// The time surface: Duration, Instant, WallTime, and Task.sleep.

func dependencyNames(result compiler.CompilationResult) []string {
	names := make([]string, 0, len(result.Dependencies))
	for _, dependency := range result.Dependencies {
		names = append(names, string(dependency))
	}
	return names
}

func TestTimeSurfaceAcceptsSettledOperations(t *testing.T) {
	assertCompiles(t, "import\n  Time from std.time\nend\nfun run(): Nil | Error do\n"+
		"    let d: Time.Duration = Time.seconds(2) + Time.milliseconds(500)\n"+
		"    let e: Time.Duration = d - Time.microseconds(1)\n"+
		"    let n: UInt64 = d.as_nanoseconds() + d.as_microseconds() + d.as_milliseconds() + d.as_seconds()\n"+
		"    let z: Time.Duration = Time.nanoseconds(0)\n"+
		"    let ordered: Bool = (d == e) or (d != e) or (d < e) or (d <= e) or (d > e) or (d >= e)\n"+
		"    let a: Time.Instant = Time.now()\n"+
		"    let b: Time.Instant = Time.now()\n"+
		"    let gap: Time.Duration = (b - a) + b.duration_since(a) + a.elapsed()\n"+
		"    let later: Bool = b >= a\n"+
		"    let w: Time.WallTime = try Time.wall_time()\n"+
		"    let s: Int64 = w.seconds()\n"+
		"    let f: UInt32 = w.nanosecond()\n"+
		"    let same: Bool = (w == w) and (w <= w)\n"+
		"    return nil\n"+
		"end\n"+
		"let r: Nil | Error = run()\n")
}

func TestTimeSurfaceRejectsUnlistedOperations(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"import\n  Time from std.time\nend\nlet d: Time.Duration = Time.seconds(1)\nlet x: Time.Duration = d * d", "operator * is not defined for Duration"},
		{"import\n  Time from std.time\nend\nlet d: Time.Duration = Time.seconds(1)\nlet x: Time.Duration = d / d", "operator / is not defined for Duration"},
		{"import\n  Time from std.time\nend\nlet d: Time.Duration = Time.seconds(1)\nlet x: Bool = d < 5", "requires identical operand types"},
		{"import\n  Time from std.time\nend\nlet n: Int32 = 5\nlet d: Time.Duration = Time.seconds(n)", "expected UInt64"},
		{"import\n  Time from std.time\nend\nlet d: Time.Duration = Time.hours(1)", "declaration hours is private to module std/time"},
		{"import\n  Time from std.time\nend\nlet d: Time.Duration = Time.seconds(1)\nlet x: UInt64 = d.as_hours()", "Duration has no method as_hours"},
		{"import\n  Time from std.time\nend\nlet d: Time.Duration = Time.seconds(1)\nlet x: UInt64 = d.to<UInt64>()", "Duration has no generic method to"},
		{"import\n  Time from std.time\nend\nlet x: Time.Duration = -Time.seconds(1)", "negation requires a signed type"},
		{"import\n  Time from std.time\nend\nlet i: Time.Instant = Time.Instant(5)", "Instant is not a constructible type"},
		{"import\n  Time from std.time\nend\nlet i: Time.Instant = Time.now()\nlet x: Time.Instant = i + Time.seconds(1)", "requires identical operand types"},
		{"import\n  Time from std.time\nend\nlet a: Time.Instant = Time.now()\nlet x: Time.Instant = a + a", "operator + is not defined for Instant"},
		{"import\n  Time from std.time\nend\nlet i: Time.Instant = Time.now()\nlet x: UInt64 = i.as_nanoseconds()", "Instant has no method as_nanoseconds"},
		{"import\n  Time from std.time\nend\nfun f(): Nil | Error do\n    let w: Time.WallTime = try Time.wall_time()\n    let x: Time.Duration = w - w\n    return nil\nend", "operator - is not defined for WallTime"},
		{"import\n  Time from std.time\nend\nfun f(): Nil | Error do\n    let w: Time.WallTime = try Time.wall_time()\n    let x: Time.Instant = w.to_instant()\n    return nil\nend", "WallTime has no method to_instant"},
		{"import\n  Time from std.time\nend\nlet d: Time.Duration = Time.seconds(1)\nprint(d)", "print does not support Duration"},
		{"import\n  Time from std.time\nend\nTime.sleep(5)", "expected Duration"},
		{"import\n  Time from std.time\nend\nfun spin(): Int32 do\n    while true do\n        Time.sleep(Time.milliseconds(1))\n    end\n    return 0\nend\nlet t: Task<Int32> | Error = spawn spin()", "while true loop must execute Task.yield()"},
	} {
		assertRejects(t, testCase.source, testCase.want)
	}
	// The former time type names are ordinary user-declarable names now.
	for _, name := range []string{"Duration", "Instant", "WallTime"} {
		assertCompiles(t, "type "+name+" is UInt64")
	}
}

// WallTime alone selects no libuv; Instant selects the native bootstrap but
// no scheduler; Time.sleep selects the scheduler and the event bridge.
func TestTimeComponentDemand(t *testing.T) {
	wall := assertCompiles(t, "import\n  Time from std.time\nend\nfun f(): Nil | Error do\n    let w: Time.WallTime = try Time.wall_time()\n    return nil\nend\nlet r: Nil | Error = f()\n")
	if !hasFile(wall, "hexal/time.h") || !hasFile(wall, "hexal/time.c") {
		t.Fatalf("WallTime program must emit the time pair: %v", sortedKeys(wall.Files))
	}
	if len(wall.Dependencies) != 1 || wall.Dependencies[0] != compiler.RuntimeMimalloc || strings.Contains(wall.Files["hexal/time.c"], "uv") ||
		strings.Contains(hexalH(t, wall), "hex_runtime_native_init") || hasFile(wall, "hexal/concurrency.c") {
		t.Fatalf("WallTime alone must select no libuv, bootstrap, or scheduler: %v %v", dependencyNames(wall), sortedKeys(wall.Files))
	}

	instant := assertCompiles(t, "import\n  Time from std.time\nend\nlet a: Time.Instant = Time.now()\nlet d: Time.Duration = a.elapsed()\n")
	if !slices.Equal(dependencyNames(instant), []string{"libuv", "mimalloc"}) {
		t.Fatalf("Instant.now must select libuv and mimalloc; got %v", dependencyNames(instant))
	}
	if hasFile(instant, "hexal/concurrency.c") || hasFile(instant, "hexal/event.c") {
		t.Fatalf("Instant.now must select neither scheduler nor event bridge: %v", sortedKeys(instant.Files))
	}
	if strings.Count(instant.Files["hexal/time.c"], "uv_hrtime()") != 1 || !strings.Contains(instant.Files["hexal/runtime.c"], "uv_replace_allocator") {
		t.Fatalf("Instant.now must read uv_hrtime once and install the bootstrap:\n%s\n%s", instant.Files["hexal/time.c"], instant.Files["hexal/runtime.c"])
	}
	main := rootC(t, instant)
	bootstrap := strings.Index(main, "hex_runtime_native_init();")
	if bootstrap < 0 || bootstrap > strings.Index(main, "hex_instant_now()") || strings.Contains(main, "hex_scheduler_init") {
		t.Fatalf("root must bootstrap libuv before Instant.now and start no scheduler:\n%s", main)
	}

	sleep := assertCompiles(t, "import\n  Time from std.time\nend\nTime.sleep(Time.milliseconds(1))\n")
	if !hasFile(sleep, "hexal/concurrency.c") || !strings.Contains(sleep.Files["hexal/event.c"], "void hex_task_sleep(hex_duration duration) {") {
		t.Fatalf("Task.sleep must select the scheduler and the event timer: %v", sortedKeys(sleep.Files))
	}
	main = rootC(t, sleep)
	if strings.Index(main, "hex_runtime_native_init();") > strings.Index(main, "hex_scheduler_init();") {
		t.Fatalf("root must bootstrap libuv before the scheduler:\n%s", main)
	}

	quiet := assertCompiles(t, "let value: Int32 = 1 + 1\n")
	if hasFile(quiet, "hexal/time.h") || hasFile(quiet, "hexal/time.c") || len(quiet.Dependencies) != 0 {
		t.Fatalf("a program without time emits no time artifacts: %v", sortedKeys(quiet.Files))
	}
	// IO and print without Task keep their direct path and select no libuv.
	printOnly := assertCompiles(t, "print(42)\n")
	if slices.Contains(dependencyNames(printOnly), "libuv") || strings.Contains(hexalH(t, printOnly), "hex_runtime_native_init") {
		t.Fatalf("print alone must not select libuv: %v", dependencyNames(printOnly))
	}
}

func TestTimeGeneratedCContract(t *testing.T) {
	result := assertCompiles(t, "import\n  Time from std.time\nend\nfun f(): Nil | Error do\n"+
		"    let w: Time.WallTime = try Time.wall_time()\n"+
		"    let a: Time.Instant = Time.now()\n"+
		"    Time.sleep(Time.nanoseconds(1) + a.elapsed())\n"+
		"    return nil\n"+
		"end\n"+
		"let r: Nil | Error = f()\n")
	header := result.Files["hexal/time.h"]
	for _, required := range []string{"typedef uint64_t hex_duration;", "typedef uint64_t hex_instant;", "int64_t seconds;", "uint32_t nanosecond;"} {
		if !strings.Contains(header, required) {
			t.Fatalf("time.h lacks %q:\n%s", required, header)
		}
	}
	if strings.Contains(header, "uv_") || strings.Contains(header, "uv.h") {
		t.Fatalf("time.h must expose no libuv name:\n%s", header)
	}
	source := result.Files["hexal/time.c"]
	for _, required := range []string{
		"timespec_get(&now, TIME_UTC) != TIME_UTC",
		"ckd_mul(&result, value, scale)",
		"[Runtime Error] duration overflow\\n",
		"[Runtime Error] duration underflow\\n",
		"[Runtime Error] invalid instant subtraction\\n",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("time.c lacks %q:\n%s", required, source)
		}
	}
	if strings.Contains(source, "uv_gettimeofday") || strings.Contains(source, "clock_gettime") || strings.Contains(source, "GetSystemTime") {
		t.Fatalf("wall time must use only timespec_get:\n%s", source)
	}
	event := result.Files["hexal/event.c"]
	for _, required := range []string{
		"uint64_t millis = nanoseconds / 1000000u;",
		"return nanoseconds % 1000000u == 0 ? millis : millis + 1;",
		"if (duration > (uint64_t)INT64_MAX) {\n        hex_runtime_trap(\"[Runtime Error] sleep duration too large\\n\");",
		"uint64_t elapsed = uv_hrtime() - sleep->start;",
		"uv_close((uv_handle_t *)timer, hex_event_sleep_closed);",
		"[Runtime Error] task sleep failed\\n",
	} {
		if !strings.Contains(event, required) {
			t.Fatalf("event.c sleep lacks %q:\n%s", required, event)
		}
	}
	// Zero returns before any submission; the bound check precedes the
	// monotonic start and the park.
	sleepBody := event[strings.Index(event, "void hex_task_sleep(hex_duration duration) {"):]
	if strings.Index(sleepBody, "if (duration == 0)") > strings.Index(sleepBody, "INT64_MAX") ||
		strings.Index(sleepBody, "INT64_MAX") > strings.Index(sleepBody, "hex_event_park(") {
		t.Fatalf("sleep must return on zero and reject oversized durations before submission:\n%s", sleepBody)
	}
	// The expiry callback never wakes the Task; only the close callback does.
	fired := event[strings.Index(event, "static void hex_event_sleep_fired"):strings.Index(event, "static void hex_event_sleep_start")]
	if strings.Contains(fired, "hex_task_event_wake") || strings.Contains(event, "uv_sleep") || strings.Contains(event, "start + ") {
		t.Fatalf("sleep must wake only from its close callback, never uv_sleep, and never form an absolute target:\n%s", event)
	}
	adapter := rootH(t, result)
	if !strings.Contains(adapter, ".hex_m_kind = (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Unsupported }") {
		t.Fatalf("WallTime.now adapter lacks its Unsupported kind:\n%s", adapter)
	}
	if !strings.Contains(adapter, ".hex_m_message = hex_error_message(hex_text_heap(&hex_lit_") || !strings.Contains(result.Files["hexal/string.c"], "byte_length = 29") {
		t.Fatalf("WallTime.now failure message must be the static 29-byte literal:\n%s", adapter)
	}
}
