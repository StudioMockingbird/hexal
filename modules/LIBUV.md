# libuv build input

- Upstream: https://github.com/libuv/libuv
- Release: v1.52.1
- Annotated tag object: 48dbd851fe4ad7d1252b75ac907c725a437e1634
- Peeled commit: 1cfa32ff59c076ffb6ed735bbc8c18361558661f
- Source archive: https://dist.libuv.org/dist/v1.52.1/libuv-v1.52.1.tar.gz
- Archive size: 1354296 bytes
- Archive SHA-256: 66d511b9e6e334c0e62279eb234fbfb2b3110b1479c09b95b44c7afca8cff9e7
- Qualified target: x86_64-windows-gnu
- Qualification backend: Zig 0.16.0
- C dialect: C11
- Optimization: `-O2`
- Compile definitions: `WIN32_LEAN_AND_MEAN`, `_WIN32_WINNT=0x0A00`, `_CRT_DECLARE_NONSTDC_NAMES=0`, `_CRT_SECURE_NO_WARNINGS`
- Compile option: `-fno-strict-aliasing`
- System libraries: psapi, user32, advapi32, iphlpapi, userenv, ws2_32, dbghelp, ole32, shell32
- Build output: the driver compiles the qualified source list with `zig cc -std=c11 -target x86_64-windows-gnu -O2`, creates `libuv.a` with the bundled archiver inside each isolated build staging directory, and statically links it.
- Source list:

```text
fs-poll.c idna.c inet.c random.c strscpy.c strtok.c thread-common.c
threadpool.c timer.c uv-common.c uv-data-getter-setters.c version.c
win/async.c win/core.c win/detect-wakeup.c win/dl.c win/error.c win/fs.c
win/fs-event.c win/getaddrinfo.c win/getnameinfo.c win/handle.c
win/loop-watcher.c win/pipe.c win/thread.c win/poll.c win/process.c
win/process-stdio.c win/signal.c win/snprintf.c win/stream.c win/tcp.c
win/tty.c win/udp.c win/util.c win/winapi.c win/winsock.c
```
- Allocator handoff: generated scheduler bootstrap calls
  `uv_replace_allocator(mi_malloc, mi_realloc, mi_calloc, mi_free)` once,
  before any other libuv operation.
- Integration rule: build inputs are embedded from this pinned submodule; user builds do not invoke Git, CMake, or a downloader.
