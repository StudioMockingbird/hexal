//go:build c23

package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

func TestLibuvDependencyProbe(t *testing.T) {
	selected := requireBackend(t)
	staging := t.TempDir()
	dependency, err := materializeDependencies(staging, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err != nil {
		t.Fatal(err)
	}
	var result BuildResult
	if err := compileNativeDependencies(selected, staging, dependency, &result); err != nil {
		if len(result.Commands) > 0 {
			t.Fatalf("%v\ncommand: %v\nstderr: %s", err, result.Commands[len(result.Commands)-1].Arguments, result.Commands[len(result.Commands)-1].Stderr)
		}
		t.Fatal(err)
	}
	if len(dependency.sources) != len(libuvWindowsSources) {
		t.Fatalf("sources = %d, want %d", len(dependency.sources), len(libuvWindowsSources))
	}
	if _, err := os.Stat(dependency.objects[0]); err != nil {
		t.Fatal(err)
	}
	if dependency.libuvArchive == "" || len(dependency.linkObjects) != 1 || dependency.linkObjects[0] != dependency.libuvArchive {
		t.Fatalf("libuv link artifact = %v, archive = %q", dependency.linkObjects, dependency.libuvArchive)
	}
	if _, err := os.Stat(dependency.libuvArchive); err != nil {
		t.Fatalf("libuv static archive was not created: %v", err)
	}
}

func TestLibuvTypedNetworkProbe(t *testing.T) {
	selected := requireBackend(t)
	staging := t.TempDir()
	native, err := materializeDependencies(staging, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err != nil {
		t.Fatal(err)
	}
	var result BuildResult
	if err := compileNativeDependencies(selected, staging, native, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	fixture := filepath.Join(staging, "typed_network_probe.c")
	const source = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <uv.h>

static uv_loop_t tcp_loop;
static uv_tcp_t tcp_server;
static uv_tcp_t tcp_client;
static uv_tcp_t tcp_peer;
static uv_connect_t tcp_connect;
static uv_timer_t tcp_timer;
static uv_buf_t tcp_message = { .base = "x", .len = 1 };
static int tcp_result;

static void tcp_alloc(uv_handle_t *handle, size_t size, uv_buf_t *buffer) {
    (void)handle;
    buffer->base = malloc(size);
    buffer->len = size;
}

static void tcp_close(uv_handle_t *handle) { (void)handle; }
static void tcp_write_done(uv_write_t *request, int status) {
    if (status != 0) tcp_result = -1;
    free(request);
}
static void tcp_read(uv_stream_t *stream, ssize_t count, const uv_buf_t *buffer) {
    if (count == 1 && buffer->base[0] == 'x') tcp_result = 1;
    else if (count < 0) tcp_result = -1;
    free(buffer->base);
    uv_read_stop(stream);
    uv_close((uv_handle_t *)stream, tcp_close);
    uv_close((uv_handle_t *)&tcp_client, tcp_close);
    uv_close((uv_handle_t *)&tcp_server, tcp_close);
    uv_close((uv_handle_t *)&tcp_timer, tcp_close);
}
static void tcp_connection(uv_stream_t *server, int status) {
    if (status != 0 || uv_tcp_init(&tcp_loop, &tcp_peer) != 0 || uv_accept(server, (uv_stream_t *)&tcp_peer) != 0) {
        tcp_result = -1;
        return;
    }
    if (uv_read_start((uv_stream_t *)&tcp_peer, tcp_alloc, tcp_read) != 0) tcp_result = -1;
}
static void tcp_connected(uv_connect_t *request, int status) {
    if (status != 0) {
        tcp_result = -1;
        return;
    }
    uv_write_t *write = malloc(sizeof(*write));
    if (write == nullptr || uv_write(write, request->handle, &tcp_message, 1, tcp_write_done) != 0) {
        free(write);
        tcp_result = -1;
    }
}
static void tcp_timeout(uv_timer_t *timer) {
    (void)timer;
    tcp_result = -1;
    uv_close((uv_handle_t *)&tcp_client, tcp_close);
    uv_close((uv_handle_t *)&tcp_server, tcp_close);
    uv_close((uv_handle_t *)&tcp_timer, tcp_close);
}

static int run_tcp(void) {
    struct sockaddr_in address;
    int address_length = sizeof(address);
    if (uv_loop_init(&tcp_loop) != 0 || uv_tcp_init(&tcp_loop, &tcp_server) != 0 || uv_tcp_init(&tcp_loop, &tcp_client) != 0) return 10;
    if (uv_ip4_addr("127.0.0.1", 0, &address) != 0 || uv_tcp_bind(&tcp_server, (const struct sockaddr *)&address, 0) != 0 || uv_listen((uv_stream_t *)&tcp_server, 1, tcp_connection) != 0) return 11;
    if (uv_tcp_getsockname(&tcp_server, (struct sockaddr *)&address, &address_length) != 0) return 12;
    if (uv_timer_init(&tcp_loop, &tcp_timer) != 0 || uv_timer_start(&tcp_timer, tcp_timeout, 1000, 0) != 0) return 13;
    tcp_result = 0;
    if (uv_tcp_connect(&tcp_connect, &tcp_client, (const struct sockaddr *)&address, tcp_connected) != 0) return 14;
    uv_run(&tcp_loop, UV_RUN_DEFAULT);
    if (uv_loop_close(&tcp_loop) != 0) return 15;
    return tcp_result == 1 ? 0 : 16;
}

static uv_loop_t udp_loop;
static uv_udp_t udp_receiver;
static uv_udp_t udp_sender;
static uv_timer_t udp_timer;
static uv_buf_t udp_message = { .base = "u", .len = 1 };
static int udp_result;

static void udp_alloc(uv_handle_t *handle, size_t size, uv_buf_t *buffer) {
    (void)handle;
    buffer->base = malloc(size);
    buffer->len = size;
}
static void udp_close(uv_handle_t *handle) { (void)handle; }
static void udp_sent(uv_udp_send_t *request, int status) {
    if (status != 0) udp_result = -1;
    free(request);
}
static void udp_received(uv_udp_t *handle, ssize_t count, const uv_buf_t *buffer, const struct sockaddr *address, unsigned flags) {
    (void)handle;
    (void)address;
    (void)flags;
    if (count == 1 && buffer->base[0] == 'u') udp_result = 1;
    else if (count < 0) udp_result = -1;
    free(buffer->base);
    uv_udp_recv_stop(&udp_receiver);
    uv_close((uv_handle_t *)&udp_receiver, udp_close);
    uv_close((uv_handle_t *)&udp_sender, udp_close);
    uv_close((uv_handle_t *)&udp_timer, udp_close);
}
static void udp_timeout(uv_timer_t *timer) {
    (void)timer;
    udp_result = -1;
    uv_close((uv_handle_t *)&udp_receiver, udp_close);
    uv_close((uv_handle_t *)&udp_sender, udp_close);
    uv_close((uv_handle_t *)&udp_timer, udp_close);
}

static int run_udp(void) {
    struct sockaddr_in address;
    int address_length = sizeof(address);
    if (uv_loop_init(&udp_loop) != 0 || uv_udp_init(&udp_loop, &udp_receiver) != 0 || uv_udp_init(&udp_loop, &udp_sender) != 0) return 20;
    if (uv_ip4_addr("127.0.0.1", 0, &address) != 0 || uv_udp_bind(&udp_receiver, (const struct sockaddr *)&address, 0) != 0) return 21;
    if (uv_udp_getsockname(&udp_receiver, (struct sockaddr *)&address, &address_length) != 0 || uv_udp_recv_start(&udp_receiver, udp_alloc, udp_received) != 0) return 22;
    if (uv_timer_init(&udp_loop, &udp_timer) != 0 || uv_timer_start(&udp_timer, udp_timeout, 1000, 0) != 0) return 23;
    udp_result = 0;
    uv_udp_send_t *send = malloc(sizeof(*send));
    if (send == nullptr || uv_udp_send(send, &udp_sender, &udp_message, 1, (const struct sockaddr *)&address, udp_sent) != 0) return 24;
    uv_run(&udp_loop, UV_RUN_DEFAULT);
    if (uv_loop_close(&udp_loop) != 0) return 25;
    return udp_result == 1 ? 0 : 26;
}

int main(void) {
    int tcp = run_tcp();
    int udp = run_udp();
    if (tcp != 0 || udp != 0) return tcp != 0 ? tcp : udp;
    puts("ok");
    return 0;
}
`
	if err := os.WriteFile(fixture, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compileTranslationUnitsWithOptions(selected, staging, []string{fixture}, native.compileOptions, nil, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	objects := []string{strings.TrimSuffix(fixture, ".c") + ".o"}
	objects = append(objects, native.linkObjects...)
	output := filepath.Join(staging, "typed_network_probe"+exeSuffix())
	if err := linkObjectsWithOptions(selected, staging, objects, native.linkOptions, output, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	run, err := exec.Command(output).CombinedOutput()
	if err != nil || string(run) != "ok\r\n" {
		t.Fatalf("output = %q, error = %v", run, err)
	}
}

func TestLibuvIdleConnectionProbe(t *testing.T) {
	selected := requireBackend(t)
	staging := t.TempDir()
	native, err := materializeDependencies(staging, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err != nil {
		t.Fatal(err)
	}
	var result BuildResult
	if err := compileNativeDependencies(selected, staging, native, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	fixture := filepath.Join(staging, "idle_connection_probe.c")
	const source = `#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <tlhelp32.h>
#include <stdio.h>
#include <uv.h>

enum { idle_count = 1000 };
static uv_loop_t loop;
static uv_tcp_t listener;
static uv_tcp_t clients[idle_count];
static uv_tcp_t peers[idle_count];
static uv_connect_t connects[idle_count];
static uv_timer_t timeout;
static int accepted;
static int connected;
static int next_client;
static int initialized_clients;
static int initialized_peers;
static int failed;

static void closed(uv_handle_t *handle) { (void)handle; }
static void connected_callback(uv_connect_t *request, int status);

static void timeout_callback(uv_timer_t *handle) {
    (void)handle;
    failed = 1;
    uv_stop(&loop);
}

static void start_next_client(const struct sockaddr *address) {
    if (next_client == idle_count || failed) return;
    int index = next_client++;
    if (uv_tcp_init(&loop, &clients[index]) != 0 ||
        uv_tcp_connect(&connects[index], &clients[index], address, connected_callback) != 0) {
        failed = 1;
        return;
    }
    clients[index].data = (void *)address;
    initialized_clients++;
}

static int process_thread_count(void) {
    HANDLE snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD, 0);
    if (snapshot == INVALID_HANDLE_VALUE) return -1;
    THREADENTRY32 entry = { .dwSize = sizeof(entry) };
    int count = 0;
    if (Thread32First(snapshot, &entry)) {
        do {
            if (entry.th32OwnerProcessID == GetCurrentProcessId()) count++;
        } while (Thread32Next(snapshot, &entry));
    }
    CloseHandle(snapshot);
    return count;
}

static void connected_callback(uv_connect_t *request, int status) {
    (void)request;
    if (status != 0) failed = 1;
    else {
        connected++;
        start_next_client(request->handle->data);
    }
    if (accepted == idle_count && connected == idle_count) uv_stop(&loop);
}

static void connection_callback(uv_stream_t *server, int status) {
    if (status != 0 || accepted == idle_count) {
        failed = 1;
        return;
    }
    uv_tcp_t *peer = &peers[accepted];
    if (uv_tcp_init(&loop, peer) != 0 || uv_accept(server, (uv_stream_t *)peer) != 0) {
        failed = 1;
        return;
    }
    initialized_peers++;
    accepted++;
    if (accepted == idle_count && connected == idle_count) uv_stop(&loop);
}

int main(void) {
    struct sockaddr_in address;
    int address_length = sizeof(address);
    int before = process_thread_count();
    if (before < 0 || uv_loop_init(&loop) != 0 || uv_tcp_init(&loop, &listener) != 0) return 10;
    if (uv_ip4_addr("127.0.0.1", 0, &address) != 0 ||
        uv_tcp_bind(&listener, (const struct sockaddr *)&address, 0) != 0 ||
        uv_listen((uv_stream_t *)&listener, idle_count, connection_callback) != 0 ||
        uv_tcp_getsockname(&listener, (struct sockaddr *)&address, &address_length) != 0) return 11;
    if (uv_timer_init(&loop, &timeout) != 0 || uv_timer_start(&timeout, timeout_callback, 60000, 0) != 0) return 12;
    for (int index = 0; index < 32; index++) start_next_client((const struct sockaddr *)&address);
    uv_run(&loop, UV_RUN_DEFAULT);
    int after = process_thread_count();
    uv_timer_stop(&timeout);
    uv_close((uv_handle_t *)&timeout, closed);
    for (int index = 0; index < initialized_clients; index++) {
        uv_close((uv_handle_t *)&clients[index], closed);
    }
    for (int index = 0; index < initialized_peers; index++) {
        uv_close((uv_handle_t *)&peers[index], closed);
    }
    uv_close((uv_handle_t *)&listener, closed);
    uv_run(&loop, UV_RUN_DEFAULT);
    if (uv_loop_close(&loop) != 0) return 14;
    if (failed || accepted != idle_count || connected != idle_count || after < 0 || after - before > 4) {
        fprintf(stderr, "failed=%d accepted=%d connected=%d before=%d after=%d\\n", failed, accepted, connected, before, after);
        return 13;
    }
    puts("ok");
    return 0;
}
`
	if err := os.WriteFile(fixture, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compileTranslationUnitsWithOptions(selected, staging, []string{fixture}, native.compileOptions, nil, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	objects := []string{strings.TrimSuffix(fixture, ".c") + ".o"}
	objects = append(objects, native.linkObjects...)
	output := filepath.Join(staging, "idle_connection_probe"+exeSuffix())
	if err := linkObjectsWithOptions(selected, staging, objects, native.linkOptions, output, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	run, err := exec.Command(output).CombinedOutput()
	if err != nil || string(run) != "ok\r\n" {
		t.Fatalf("output = %q, error = %v", run, err)
	}
}

func TestLibuvCancellationProbe(t *testing.T) {
	selected := requireBackend(t)
	staging := t.TempDir()
	native, err := materializeDependencies(staging, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err != nil {
		t.Fatal(err)
	}
	var result BuildResult
	if err := compileNativeDependencies(selected, staging, native, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	fixture := filepath.Join(staging, "cancellation_probe.c")
	const source = `#include <stdatomic.h>
#include <stdio.h>
#include <uv.h>

static uv_loop_t loop;
static uv_mutex_t mutex;
static uv_cond_t cond;
static int blockers_started;
static int release_blockers;
static int blockers_done;
static int target_ran;
static int target_status;
static int running_started;
static int running_ran;
static int running_status;

static void blocker_work(uv_work_t *request) {
    (void)request;
    uv_mutex_lock(&mutex);
    blockers_started++;
    uv_cond_signal(&cond);
    while (!release_blockers) uv_cond_wait(&cond, &mutex);
    uv_mutex_unlock(&mutex);
}
static void blocker_done(uv_work_t *request, int status) {
    (void)request;
    if (status != 0) return;
    uv_mutex_lock(&mutex);
    blockers_done++;
    uv_cond_signal(&cond);
    uv_mutex_unlock(&mutex);
}
static void target_work(uv_work_t *request) {
    (void)request;
    target_ran = 1;
}
static void target_done(uv_work_t *request, int status) {
    (void)request;
    target_status = status;
}
static void running_work(uv_work_t *request) {
    (void)request;
    uv_mutex_lock(&mutex);
    running_started = 1;
    uv_cond_signal(&cond);
    uv_mutex_unlock(&mutex);
    uv_sleep(25);
    running_ran = 1;
}
static void running_done(uv_work_t *request, int status) {
    (void)request;
    running_status = status;
}

int main(void) {
    uv_work_t blockers[4] = {0};
    uv_work_t target = {0};
    uv_work_t running = {0};
    if (uv_loop_init(&loop) != 0 || uv_mutex_init(&mutex) != 0 || uv_cond_init(&cond) != 0) return 10;
    for (int index = 0; index < 4; index++) {
        if (uv_queue_work(&loop, &blockers[index], blocker_work, blocker_done) != 0) return 11;
    }
    uv_mutex_lock(&mutex);
    for (int attempt = 0; attempt < 100 && blockers_started < 4; attempt++) {
        uv_cond_timedwait(&cond, &mutex, 10000000);
    }
    if (blockers_started < 4) {
        uv_mutex_unlock(&mutex);
        return 18;
    }
    uv_mutex_unlock(&mutex);
    if (uv_queue_work(&loop, &target, target_work, target_done) != 0 || uv_cancel((uv_req_t *)&target) != 0) return 12;
    uv_mutex_lock(&mutex);
    release_blockers = 1;
    uv_cond_broadcast(&cond);
    uv_mutex_unlock(&mutex);
    uv_run(&loop, UV_RUN_DEFAULT);
    if (blockers_done != 4 || target_ran != 0 || target_status != UV_ECANCELED) return 13;

    if (uv_queue_work(&loop, &running, running_work, running_done) != 0) return 14;
    uv_mutex_lock(&mutex);
    for (int attempt = 0; attempt < 100 && !running_started; attempt++) {
        uv_cond_timedwait(&cond, &mutex, 10000000);
    }
    if (!running_started) {
        uv_mutex_unlock(&mutex);
        return 19;
    }
    uv_mutex_unlock(&mutex);
    if (uv_cancel((uv_req_t *)&running) != UV_EBUSY) return 15;
    uv_run(&loop, UV_RUN_DEFAULT);
    if (running_ran != 1 || running_status != 0) return 16;

    if (uv_loop_close(&loop) != 0) return 17;
    uv_cond_destroy(&cond);
    uv_mutex_destroy(&mutex);
    puts("ok");
    return 0;
}
`
	if err := os.WriteFile(fixture, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compileTranslationUnitsWithOptions(selected, staging, []string{fixture}, native.compileOptions, nil, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	objects := []string{strings.TrimSuffix(fixture, ".c") + ".o"}
	objects = append(objects, native.linkObjects...)
	output := filepath.Join(staging, "cancellation_probe"+exeSuffix())
	if err := linkObjectsWithOptions(selected, staging, objects, native.linkOptions, output, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	run, err := exec.Command(output).CombinedOutput()
	if err != nil || string(run) != "ok\r\n" {
		t.Fatalf("output = %q, error = %v", run, err)
	}
}
func TestLibuvEventRuntimeProbe(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "import\n  Io from std.io\nend\nfun helper(): Int32 do\n    return 7\nend\nfun run(): Int32 | Error do\n    out: Io.IO := try Io.stdout()\n    w: Size | Error := out.write(\"ok\".bytes())\n    task: Task<Int32> := try spawn helper()\n    return task.join()\nend\nvalue: Int32 | Error := run()\n")
	result, err := Build(BuildOptions{Root: dir})
	if err != nil {
		if len(result.Commands) > 0 {
			last := result.Commands[len(result.Commands)-1]
			t.Fatalf("%v\ncommand: %v\nstderr: %s", err, last.Arguments, last.Stderr)
		}
		t.Fatal(err)
	}
	output, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil || string(output) != "ok" {
		t.Fatalf("output = %q, error = %v", output, err)
	}
}

func TestLibuvSchedulerContentionProbe(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	source := `import
  Io from std.io
end

fun blocked(): Int32 | Error do
    input: Io.IO := try Io.stdin()
    h: Heap := Heap()
    buffer: List<Byte> := List<Byte>(h)
    defer buffer.free(h)
    result: Size | EoS | Error := input.read(buffer, 1)
    return 0
end
fun ready(): Int32 do
    return 7
end
fun run(): Int32 | Error do
    first: Task<Int32 | Error> := try spawn blocked()
    second: Task<Int32 | Error> := try spawn blocked()
    third: Task<Int32 | Error> := try spawn blocked()
    fourth: Task<Int32 | Error> := try spawn blocked()
    fifth: Task<Int32 | Error> := try spawn blocked()
    sixth: Task<Int32 | Error> := try spawn blocked()
    ready_task: Task<Int32> := try spawn ready()
    return ready_task.join()
end
value: Int32 | Error := run()
`
	writeSource(t, dir, "main.hex", source)
	result, err := Build(BuildOptions{Root: dir})
	if err != nil {
		if len(result.Commands) > 0 {
			last := result.Commands[len(result.Commands)-1]
			t.Fatalf("%v\ncommand: %v\nstderr: %s", err, last.Arguments, last.Stderr)
		}
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	command := exec.Command(result.Executable)
	command.Stdin = reader
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("contention fixture failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		<-finished
		t.Fatal("ready Task did not run while libuv work requests were blocked")
	}
}

func TestLibuvEventFoundationProbe(t *testing.T) {
	selected := requireBackend(t)
	compileResult := compiler.Compile(map[string]string{
		"main.hex": "import\n  Io from std.io\nend\nfun helper(): Int32 do\n    return 7\nend\nfun run(): Int32 | Error do\n    out: Io.IO := try Io.stdout()\n    w: Size | Error := out.write(\"ok\".bytes())\n    task: Task<Int32> := try spawn helper()\n    return task.join()\nend\nvalue: Int32 | Error := run()\n",
	}, "main.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if len(compileResult.Stderr) > 0 {
		t.Fatalf("Hexal compilation failed: %v", compileResult.Stderr)
	}
	staging := t.TempDir()
	if _, err := materialize(staging, compileResult.Files); err != nil {
		t.Fatal(err)
	}
	native, err := materializeDependencies(staging, compileResult.Dependencies)
	if err != nil {
		t.Fatal(err)
	}
	var result BuildResult
	if err := compileNativeDependencies(selected, staging, native, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	fixture := filepath.Join(staging, "event_foundation_probe.c")
	const source = `#include "hexal/event.h"
#include <stdio.h>
#include <stdlib.h>
#include <stdatomic.h>
#include <winsock2.h>
#include <ws2tcpip.h>
#include <uv.h>

typedef struct probe_wait {
    uv_mutex_t mutex;
    uv_cond_t cond;
    int waiting;
} probe_wait;
typedef struct probe_state {
    probe_wait wait;
    int value;
} probe_state;
static _Thread_local hex_task current_task;

hex_task *hex_task_current(void) { return &current_task; }
void hex_task_event_arm(hex_task *value, void *pending) { value->pending_park = pending; }
void hex_task_event_wake(hex_task *value) {
    probe_state *state = (probe_state *)value->args;
    uv_mutex_lock(&state->wait.mutex);
    state->wait.waiting = 1;
    uv_cond_signal(&state->wait.cond);
    uv_mutex_unlock(&state->wait.mutex);
}
void hex_task_event_suspend(hex_task *value) {
    probe_state *state = (probe_state *)value->args;
    uv_mutex_lock(&state->wait.mutex);
    while (!state->wait.waiting) uv_cond_wait(&state->wait.cond, &state->wait.mutex);
    state->wait.waiting = 0;
    uv_mutex_unlock(&state->wait.mutex);
}
[[noreturn]] void hex_runtime_trap(const char *message) {
    fputs(message, stderr);
    exit(2);
}

static void fail(void *context) { *(int *)context = -1; }
static void work(void *context) { *(int *)context = 2; }
static void parallel_work(void *context) { ((probe_state *)context)->value = 5; }
static void parallel_fail(void *context) { ((probe_state *)context)->value = -1; }
static void run_parallel(void *context) {
    probe_state *state = (probe_state *)context;
    current_task.args = state;
    hex_event_work_call(parallel_work, parallel_fail, state);
}
static _Atomic(int) slow_started;
static void slow_work(void *context) {
    atomic_store(&slow_started, 1);
    uv_sleep(25);
    ((probe_state *)context)->value = 6;
}
static void run_slow(void *context) {
    probe_state *state = (probe_state *)context;
    current_task.args = state;
    hex_event_work_call(slow_work, parallel_fail, state);
}

int main(void) {
    WSADATA data;
    if (WSAStartup(MAKEWORD(2, 2), &data) != 0) return 10;
    probe_state state = {0};
    if (uv_mutex_init(&state.wait.mutex) != 0 || uv_cond_init(&state.wait.cond) != 0) return 11;
    current_task.args = &state;
    hex_event_runtime_init();

    int value = 0;
    hex_event_work_call(work, fail, &value);
    if (value != 2) return 13;

    enum { parallel_count = 64 };
    uv_thread_t threads[parallel_count];
    probe_state parallel[parallel_count] = {0};
    for (int index = 0; index < parallel_count; index++) {
        if (uv_mutex_init(&parallel[index].wait.mutex) != 0 || uv_cond_init(&parallel[index].wait.cond) != 0) return 21;
        if (uv_thread_create(&threads[index], run_parallel, &parallel[index]) != 0) return 22;
    }
    for (int index = 0; index < parallel_count; index++) {
        uv_thread_join(&threads[index]);
        if (parallel[index].value != 5) return 23;
        uv_cond_destroy(&parallel[index].wait.cond);
        uv_mutex_destroy(&parallel[index].wait.mutex);
    }

    probe_state slow = {0};
    if (uv_mutex_init(&slow.wait.mutex) != 0 || uv_cond_init(&slow.wait.cond) != 0) return 26;
    uv_thread_t slow_thread;
    if (uv_thread_create(&slow_thread, run_slow, &slow) != 0) return 27;
    for (int attempt = 0; attempt < 100 && atomic_load(&slow_started) == 0; attempt++) uv_sleep(1);
    uv_thread_join(&slow_thread);
    if (slow.value != 6) return 28;
    uv_cond_destroy(&slow.wait.cond);
    uv_mutex_destroy(&slow.wait.mutex);
    uv_cond_destroy(&state.wait.cond);
    uv_mutex_destroy(&state.wait.mutex);
    WSACleanup();
    puts("ok");
    return 0;
}
`
	if err := os.WriteFile(fixture, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	eventSource := filepath.Join(staging, "hexal", "event.c")
	if err := compileTranslationUnitsWithOptions(selected, staging, []string{eventSource, fixture}, native.compileOptions, nil, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	objects := []string{strings.TrimSuffix(eventSource, ".c") + ".o", strings.TrimSuffix(fixture, ".c") + ".o"}
	objects = append(objects, native.linkObjects...)
	output := filepath.Join(staging, "event_foundation_probe"+exeSuffix())
	if err := linkObjectsWithOptions(selected, staging, objects, native.linkOptions, output, &result); err != nil {
		failWithLastCommand(t, &result, err)
	}
	run, err := exec.Command(output).CombinedOutput()
	if err != nil || string(run) != "ok\r\n" {
		t.Fatalf("output = %q, error = %v", run, err)
	}
}

func failWithLastCommand(t *testing.T, result *BuildResult, err error) {
	t.Helper()
	if len(result.Commands) > 0 {
		last := result.Commands[len(result.Commands)-1]
		t.Fatalf("%v\ncommand: %v\nstderr: %s", err, last.Arguments, last.Stderr)
	}
	t.Fatal(err)
}
