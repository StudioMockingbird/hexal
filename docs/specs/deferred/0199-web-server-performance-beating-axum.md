# RFC 0199: Web Server — Performance and Beating Axum

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server ecosystem
  (RFCs 0210, 0194, 0195, 0196, 0197, 0198) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0194 (web server lowering),
  RFC 0144 (high-throughput network runtime), and the implemented RFCs 0145
  (libuv runtime), 0146 (mimalloc), and 0168 (libuv capability arc)
- Coordinates with: RFC 0195 (TLS), RFC 0196 (Redis), RFC 0197 (PostgreSQL),
  and RFC 0198 (HTTP parsing) for the complete performance picture
- Does not add: server functionality, protocol changes, or new types

## Motivation

Performance is the primary differentiator for a systems language. If a Hexal
web server is slower than Axum (Rust) or Go's net/http, there is no reason
to use Hexal for web servers. This RFC defines the performance targets,
measurement methodology, and optimization strategy.

## Performance targets

| Metric | Target | Rationale |
| --- | --- | --- |
| Requests/sec (plaintext) | ≥ Axum | Baseline parity |
| Requests/sec (TLS) | ≥ Axum | Encrypted performance parity |
| Latency p50 | ≤ 0.5 ms | Sub-millisecond for simple handlers |
| Latency p99 | ≤ 5 ms | Tail latency for production workloads |
| Latency p99.9 | ≤ 20 ms | Extreme tail latency |
| Memory per connection | ≤ 2 KB | 10K concurrent connections = 20 MB |
| Startup time | ≤ 10 ms | Fast cold starts |
| Binary size (stripped) | ≤ 2 MB | Reasonable deployment artifact |
| CPU utilization (saturated) | ≥ 90% | Near-linear throughput |

## Benchmark categories

### Micro-benchmarks

| Benchmark | What it measures | Target |
| --- | --- | --- |
| `bench_http_parse` | HTTP request parsing throughput | ≥ 500K req/sec |
| `bench_http_serialize` | HTTP response serialization throughput | ≥ 500K resp/sec |
| `bench_header_lookup` | Case-insensitive header lookup | ≤ 100 ns per lookup |
| `bench_chunk_decode` | Chunked transfer decoding | ≥ 200 MB/sec |
| `bench_string_concat` | String concatenation (hot path) | ≤ 50 ns per concat |
| `bench_dict_insert` | Dict insert/lookup (hot path) | ≤ 200 ns per operation |

### Throughput benchmarks

| Benchmark | What it measures | Target |
| --- | --- | --- |
| `bench_http_plaintext` | Requests/sec (plaintext HTTP/1.1) | ≥ Axum |
| `bench_http_tls` | Requests/sec (TLS 1.3) | ≥ Axum |
| `bench_http_keepalive` | Requests/sec with keep-alive | ≥ Axum |
| `bench_http_pipeline` | Requests/sec with pipelining | ≥ Axum |
| `bench_http_chunked` | Streaming response throughput | ≥ Axum |

### Latency benchmarks

| Benchmark | What it measures | Target |
| --- | --- | --- |
| `bench_latency_simple` | Latency for simple 200 OK | p50 ≤ 0.5 ms |
| `bench_latency_json` | Latency for JSON response | p50 ≤ 1 ms |
| `bench_latency_db` | Latency for Redis + response | p50 ≤ 2 ms |
| `bench_latency_tls` | TLS handshake + response | p50 ≤ 5 ms |

### Memory benchmarks

| Benchmark | What it measures | Target |
| --- | --- | --- |
| `bench_memory_idle` | Memory with idle connections | ≤ 2 KB per connection |
| `bench_memory_active` | Memory under load | ≤ 4 KB per connection |
| `bench_memory_alloc_rate` | Allocation rate under load | ≤ 100 KB/sec |

### Scalability benchmarks

| Benchmark | What it measures | Target |
| --- | --- | --- |
| `bench_concurrent_1k` | Throughput at 1K connections | ≥ 90% of single-connection |
| `bench_concurrent_10k` | Throughput at 10K connections | ≥ 80% of single-connection |
| `bench_concurrent_100k` | Throughput at 100K connections | ≥ 60% of single-connection |

## Measurement methodology

### Tooling

- **Benchmark harness**: Go `testing.B` or custom C harness.
- **Load generator**: `wrk`, `oha`, or `bombardier` for HTTP benchmarks.
- **Profiler**: `perf` (Linux), `Instruments` (macOS), `VTune` (cross-platform).
- **Memory profiler**: Valgrind Massif, mimalloc stats.
- **Flame graph**: `flamegraph.pl` or `speedscope`.

### Methodology

1. **Warm-up**: 3 seconds before measurement.
2. **Duration**: 30 seconds per benchmark.
3. **Connections**: 100 concurrent connections, HTTP keep-alive.
4. **Threads**: Match CPU cores (typically 4-8).
5. **Request size**: Small GET request (< 256 bytes).
6. **Response size**: Small JSON response (< 1 KB).
7. **TLS**: TLS 1.3 with AES-256-GCM.
8. **Isolation**: No other workloads on the machine.
9. **Repetition**: 5 runs, report median and p99.

### Axum baseline

Axum benchmarks use:
- `axum` 0.8.x with `tokio` runtime.
- `hyper` 1.x HTTP implementation.
- `rustls` TLS.
- Same hardware, same OS, same request/response payloads.

### Comparison format

| Metric | Hexal | Axum | Ratio |
| --- | --- | --- | --- |
| Requests/sec (plaintext) | ? | ? | ? |
| Requests/sec (TLS) | ? | ? | ? |
| Latency p50 (ms) | ? | ? | ? |
| Latency p99 (ms) | ? | ? | ? |
| Memory per connection (KB) | ? | ? | ? |

## Optimization strategy

### Phase 1: Foundation (Week 1-2)

1. **Zero-copy header parsing**: Parse headers in-place from the read
   buffer; avoid copying header names and values.
2. **Stack-allocated parser**: HTTP parser state on the stack, no heap
   allocation for parsing.
3. **Batch I/O**: Read as many complete requests as possible from the
   socket buffer before yielding to the event loop.
4. **Connection pre-forking**: Pre-fork worker processes to avoid fork
   overhead per connection.

### Phase 2: Hot Path (Week 3-4)

5. **Header lookup optimization**: Use a perfect hash table or sorted
   array with binary search for header lookup.
6. **String interning**: Intern frequently used header names and values
   to avoid repeated allocation.
7. **Buffer pooling**: Pool read/write buffers to avoid per-request
   allocation.
8. **Zero-copy response**: Stream response directly from the handler
   without intermediate copying.

### Phase 3: TLS (Week 5-6)

9. **TLS session caching**: Cache TLS sessions to avoid full handshake
   on reconnection.
10. **TLS hardware acceleration**: Use AES-NI and SHA-NI instructions
    when available.
11. **TLS buffer optimization**: Use BearSSL's zero-copy mode for small
    responses.

### Phase 4: Scheduling (Week 7-8)

12. **Work-stealing**: Balance load across worker threads using
    work-stealing queues.
13. **Connection migration**: Move connections between workers to balance
    load.
14. **Back-pressure**: Apply back-pressure when the accept queue is full.
15. **Adaptive timeouts**: Adjust timeouts based on connection age and
    activity.

### Phase 5: Memory (Week 9-10)

16. **mimalloc integration**: Use mimalloc for all allocations (already
    done via RFC 0146).
17. **Arena allocation**: Use arena allocators for request-scoped data.
18. **Copy-on-write**: Share read-only data (headers, static responses)
    across connections.
19. **Memory-mapped files**: Use mmap for static file serving.

## Profiling workflow

### 1. Establish baseline

```bash
# Run HTTP benchmark
./bench_http_plaintext -threads 4 -connections 100 -duration 30s > baseline.txt

# Generate flame graph
perf record -g ./bench_http_plaintext
perf script | flamegraph.pl > baseline.svg
```

### 2. Identify bottleneck

```bash
# CPU profiling
perf top -g ./bench_http_plaintext

# Memory profiling
valgrind --tool=massif ./bench_http_plaintext
ms_print massif.out.*
```

### 3. Optimize and measure

```bash
# Run optimized version
./bench_http_plaintext_optimized -threads 4 -connections 100 -duration 30s > optimized.txt

# Compare
diff baseline.txt optimized.txt
```

### 4. Repeat

Iterate until all targets are met.

## Known Axum advantages

| Axum advantage | Hexal mitigation |
| --- | --- |
| Rust zero-cost abstractions | C23 generated code, no runtime overhead |
| Tokio work-stealing | libuv thread pool with work-stealing |
| Rustls TLS | BearSSL TLS (lighter, faster for small messages) |
| Borrow checker prevents data races | Task isolation prevents data races |
| Compile-time optimizations | Manual optimization of hot paths |

## Known Hexal advantages

| Hexal advantage | Axum disadvantage |
| --- | --- |
| No runtime overhead | Tokio runtime overhead |
| mimalloc (faster than jemalloc) | Rust allocator dependency |
| Simpler memory model | Borrow checker complexity |
| Faster compilation | Rust compilation times |
| Smaller binary size | Rust binary bloat |
| No lifetime annotations | Rust lifetime complexity |

## Demand rules

- Benchmark harness selects the benchmark component.
- The benchmark component does not select the server runtime or any
  protocol component.
- Benchmarks run standalone; they do not deploy to production.
- Benchmark results do not affect the compiler or runtime behavior.

## Required sweep

- Benchmark harness and runner in `compiler/tests/benchmarks/`;
- HTTP micro-benchmarks (parse, serialize, header lookup);
- HTTP throughput benchmarks (plaintext, TLS, keep-alive);
- HTTP latency benchmarks (simple, JSON, database);
- Memory benchmarks (idle, active, alloc rate);
- Scalability benchmarks (1K, 10K, 100K connections);
- Axum baseline comparison;
- Profiling documentation;
- `docs/reference.md` benchmark surface after explicit approval.

## Validation

This section is exhaustive:

- HTTP parser benchmark achieves ≥ 500K req/sec;
- HTTP serializer benchmark achieves ≥ 500K resp/sec;
- Header lookup benchmark achieves ≤ 100 ns per lookup;
- Plaintext HTTP throughput ≥ Axum;
- TLS 1.3 throughput ≥ Axum;
- Latency p50 ≤ 0.5 ms for simple handlers;
- Latency p99 ≤ 5 ms under load;
- Memory per connection ≤ 2 KB idle;
- Memory per connection ≤ 4 KB active;
- Startup time ≤ 10 ms;
- Binary size ≤ 2 MB stripped;
- CPU utilization ≥ 90% under saturation;
- Scalability: 1K connections ≥ 90% of single-connection;
- Scalability: 10K connections ≥ 80% of single-connection;
- Scalability: 100K connections ≥ 60% of single-connection;
- all benchmarks run in isolation without affecting production behavior;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to use `wrk`, `oha`, or `bombardier` as the primary load
   generator.
2. Whether to maintain a permanent Axum comparison suite or run it
   ad-hoc.
3. Whether to add Go net/http comparison benchmarks.
4. Whether to add Rust hyper comparison benchmarks.
5. Whether to publish benchmark results automatically or on-demand.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds benchmark harness and performance documentation only after behavior
stabilizes and with explicit user approval.
