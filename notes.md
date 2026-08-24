Do a full audit of the codebase to find opportunities for improvements/refactoring. use parallel subagents where possible.
These are the areas of focus

| # | Refactoring pass | Focus | Outcome | Risk | RoI |
|---:|---|---|---|---|---|
| 1 | Line-ending and `gofmt` hygiene | Normalize formatting and line endings. | Clean, reviewable baseline. | Low | High |
| 2 | Dead-code removal | Delete obsolete and unused code. | Smaller, clearer codebase. | Low | High |
| 3 | Legacy API removal | Remove deprecated APIs confirmed by evidence. | Fewer compatibility paths. | Medium | High |
| 4 | Determinism audit | Stabilize ordering in diagnostics, modules, maps, and generated files. | Reproducible output. | Medium | High |
| 5 | Diagnostic centralization | Consolidate diagnostic construction and formatting. | Consistent error behavior. | Medium | High |
| 6 | Naming consistency | Normalize symbols, files, types, helpers, and tests. | Easier navigation and maintenance. | Low | Medium |
| 7 | Giant-file splitting | Split files by compiler phase and responsibility. | Clearer architecture. | Medium | High |
| 8 | Parameter bundling | Replace repeated parameter groups with focused structs. | Simpler function signatures. | Medium | Medium |
| 9 | Exported-API documentation | Document the final public API shape. | Better usability and discoverability. | Low | Medium |
| 10 | Error-handling unification | Normalize propagation, classification, and returns. | Predictable failure paths. | Medium | High |
| 11 | Standard-library modernization | Replace custom utilities with standard facilities. | Less code and maintenance. | Low | Medium |
| 12 | Phase-dispatch audit | Verify parser, checker, and generator coverage of every node. | Explicit fail-closed behavior. | Medium | High |
| 13 | Import-resolution single source | Consolidate module graph and resolution logic. | One authoritative resolution path. | High | High |
| 14 | Literal-registry invariant | Centralize builtin literal/type registration. | Eliminates nil-guard bug classes. | Medium | High |
| 15 | Builtin interning unification | Unify builtin identity and add regressions. | Stable builtin behavior. | High | High |
| 16 | Ownership and cleanup audit | Review pointers, allocation, views, `defer`, and `errdefer`. | Clearer lifetime guarantees. | High | High |
| 17 | Test-helper consolidation | Remove duplicate fixtures and assertions. | Shorter, more maintainable tests. | Low | Medium |
| 18 | Reference synchronization | Align code, `reference.md`, specs, and `status.md`. | No contract drift. | Medium | High |
| 19 | Allocation and hot-path optimization | Optimize only measured bottlenecks. | Better performance without premature complexity. | High | Medium |
| 20 | Final conformance pass | Run tests, snippets, diagnostics, and generated-C comparisons. | Verified behavior-preserving refactor. | Low | Very high |








Yes. Six buckets give each spec a coherent purpose while preserving execution order.

| Bucket | Passes | Spec focus | Prompt to create the spec |
|---|---|---|---|
| 1. Safe Cleanup Baseline | 1–3 | Formatting, dead code, legacy APIs. | “Create a refactoring spec for repository hygiene, dead-code removal, and legacy API removal. Preserve behavior, establish a clean baseline, and require tests after every deletion.” |
| 2. Deterministic Compiler Surface | 4–6 | Determinism, diagnostics, naming. | “Create a refactoring spec for deterministic compiler behavior, centralized diagnostics, and naming consistency. Require stable output, preserved diagnostic meaning, and regression coverage.” |
| 3. Structural Simplification | 7–9, 11 | File splits, parameter bundling, API comments, stdlib modernization. | “Create a refactoring spec for restructuring the compiler into clear responsibility-based files, simplifying signatures, documenting exported APIs, and replacing unnecessary custom utilities with standard-library equivalents.” |
| 4. Pipeline and Semantic Invariants | 10, 12–15 | Error handling, phase dispatch, imports, literals, builtin interning. | “Create a refactoring spec for compiler pipeline and semantic invariants. Centralize error handling, make parser/checker/generator dispatch explicit, unify import resolution, enforce literal registration, and unify builtin interning without changing language semantics.” |
| 5. Lifetime, Tests, and Documentation | 16–18 | Ownership, cleanup, test helpers, reference synchronization. | “Create a refactoring spec for ownership and cleanup consistency, test-helper consolidation, and synchronization between implementation, `reference.md`, specs, and `status.md`.” |
| 6. Performance and Conformance | 19–20 | Allocation/hot paths and final validation. | “Create a final refactoring spec for evidence-based allocation and hot-path optimization followed by full conformance validation. Require benchmarks or profiling evidence and no behavior regressions.” |

Recommended dependency chain:

```text
0057 Safe Cleanup
  → 0058 Deterministic Compiler Surface
  → 0059 Structural Simplification
  → 0060 Pipeline and Semantic Invariants
  → 0061 Lifetime, Tests, and Documentation
  → 0062 Performance and Conformance
```

Each spec should include:

- scope and non-goals
- affected packages
- behavior-preservation invariants
- implementation steps
- required tests
- documentation requirements
- completion criteria
- explicit follow-up dependencies

"in 2026, if we were to make c from scratch, and be ok with removing all old baggage and cut things out ruthlessley, despite breaking changes - what all should we remove? what all would we keep? what all new things would we want to add to it, without losing the ethos of c?"

## Remove

| Feature | What changes | Why |
|---|---|---|
| Preprocessor (`#include`, macros) | Replaced by a real module system with compiled interfaces | Root cause of slow builds, ODR violations, and a whole non-hygienic shadow language living inside C |
| Undefined behavior for overflow/aliasing/uninit reads | Made defined (wrap/trap) or a compile error | UB-as-optimization-license is the single biggest source of real-world security CVEs in C code |
| Null-terminated strings as default | Replaced by length-carrying slices, C-strings kept only as an interop type | Eliminates `strlen`/buffer-overrun as a default bug class |
| Array-to-pointer decay | Arrays pass with length attached (slices) | Decay silently throws away the one piece of info (`sizeof`/length) most bugs need |
| Implicit numeric conversions & narrowing in initializers | Require explicit casts | Silent truncation/promotion is a constant footgun, especially signed/unsigned comparisons |
| `gets()` and the unsafe stdlib string family (`strcpy`, `sprintf`, …) | Deleted, not deprecated | No safe way to use them correctly; every replacement (`snprintf`, etc.) proves the originals were mistakes |
| Switch fallthrough by default | Fallthrough becomes opt-in (`fallthrough` keyword) | Forgetting `break` has caused real outages (Apple's `goto fail`-adjacent bugs, etc.) |
| Header files / forward-declaration duplication | Gone with the preprocessor; module system infers interfaces | Pure duplication tax with no semantic benefit |
| Uninitialized variables by default | Zero-init by default, `undefined` as an explicit opt-out | Uninitialized reads are cheap to eliminate and expensive to debug |
| Global `errno` | Replaced by explicit error return types | Global mutable error state doesn't compose, especially under threads |
| VLAs (variable-length arrays) on the stack | Removed | Unbounded stack allocation from attacker-controlled sizes is a known exploit vector |
| `setjmp`/`longjmp` | Removed | Non-local control flow that bypasses `defer`/cleanup and destroys optimizer reasoning |
| Trigraphs / digraphs | Removed | Dead weight from 1980s terminal constraints, actively harmful (silently rewrites code) |
| K&R implicit function declarations | Removed (C23 already did this) | "Undeclared function assumed to return int" enabled entire bug classes |
| Weak `typedef`/`enum` typing (enums as plain ints) | Enums become real, exhaustiveness-checked tagged types | `int`-flavored enums allow any garbage value silently |
| `void*`-based "generics" | Replaced by comptime generics | Casting away type information isn't generics, it's giving up |
| Makefile/autotools-style build fragmentation | Replaced by one standard build tool | Every C project reinvents a broken build system; this is pure ecosystem tax |

## Keep

| Feature | Why it stays |
|---|---|
| Small, learnable core language | The whole point — a language you can hold in your head and teach in a weekend |
| Procedural style, no mandatory OOP | Matches how systems code actually gets written; no forced class hierarchies |
| Manual memory management | The core value proposition versus GC'd languages — you control when memory moves |
| No hidden control flow (no exceptions, no operator overloading magic) | WYSIWYG cost model: reading code tells you what it does, with no invisible unwinding or dispatch |
| No hidden allocation | You can always tell where a malloc-equivalent happens by reading the call site |
| No GC, minimal/no runtime | Keeps it viable for kernels, embedded, interpreters, and anything freestanding |
| Structs, pointers, arrays as primitives | The actual vocabulary of how memory and hardware work — no reason to abstract it away |
| Value semantics | Predictable copying/ownership without a hidden reference-counting layer |
| Direct, near-1:1 mapping to machine code | You can mentally compile a function to asm; this is why C stays the systems-programming baseline |
| Fast compilation | Non-negotiable for a language meant to sit under everything else |
| Ability to write custom allocators, control memory layout precisely | Enables arenas, pools, bump allocators — the actual performance tools systems programmers reach for |
| First-class inline assembly / low-level bit manipulation | Needed for the domains C actually serves (drivers, firmware, codecs) |
| Small, stable, predictable ABI and easy FFI | C's role as the universal interop layer between every other language is worth preserving deliberately |
| Cross-platform, freestanding portability | Runs on everything from an ATmega chip to a mainframe — don't lose that |
| "Trust the programmer, give them sharp tools" spirit | The goal is moving footguns from silent to explicit, not removing programmer agency |

## Add

| Feature | What it gives you | Precedent |
|---|---|---|
| Real module system | Fast incremental builds, no textual pasting, real namespacing | Rust modules, Zig `@import` |
| Comptime (compile-time function execution + generics) | Macro-level power with type checking, no textual substitution | Zig `comptime` |
| Slices (pointer + length) as the default array/string type | Kills the `strlen`/overrun bug class by construction | Go slices, Rust `&[T]`, Zig slices |
| Tagged unions / result types with exhaustive `switch` | Structured error handling without `errno` or sentinel values | Rust `Result`/`Option`, Zig error unions |
| `defer` / `errdefer` | Scope-based cleanup that's explicit at the call site, unlike destructors | Zig, Go |
| Zero-init by default, explicit `undefined` opt-out | Removes uninitialized-read bugs for free in the common case | Zig |
| Debug-mode bounds checking, compiled out in release | Safety when you want it, zero cost when you don't | Zig `ReleaseFast` vs `Debug` |
| Explicit wrapping vs. overflow-checked arithmetic operators | Overflow behavior becomes a choice, not an assumption | Zig `+%` vs `+`, Swift `&+` |
| Fixed-width integers (`i32`, `u64`, …) as primary types | Removes the `int`/`long` size-ambiguity that's plagued portability for decades | Rust, Zig |
| Language-level atomics with a real, learnable memory model | Threading primitives that aren't "abuse `volatile` and hope" | C11 atomics, done properly this time |
| Native SIMD/vector types | First-class support for a workload that's now central, not niche | ISPC, Zig `@Vector` |
| Built-in unit testing | Testing as a toolchain feature, not a bolted-on third-party framework | Zig `test`, Rust `#[test]` |
| Standard build tool + package manager | Ends Makefile/autotools/CMake fragmentation | Cargo, Zig build system |
| First-class safety build modes (UBSan/ASan-equivalent) | Sanitizers as a supported mode, not external tooling you discover later | Zig safety modes |
| Real string type (slice-based), with a C-string type for interop | Safe strings by default, without breaking FFI to existing C libraries | Go, Rust `&str` + `CStr` |
| Structured concurrency primitives (minimal async/green threads) | Modern concurrent code without a hand-rolled thread-pool every time | Go goroutines (lighter-weight version) |
| Opt-in, lightweight ownership/lifetime checking (debatable) | Static help against use-after-free/double-free without full borrow-checker complexity | Somewhere between Zig (none) and Rust (full) |

The last row is the real fault line: it's the one addition that risks compromising "small and learnable," and it's exactly where Zig stopped short and Rust kept going. Worth noting that most of this table already exists, assembled, as Zig — this thought experiment is close to "what if C had been designed in 2016 instead of 1972."


Repo: hexal — a Hexal→C23 compiler written in Go. Read AGENTS.md first; it is binding.

TASK: Implement six specs, one at a time, in the order below. This order is
deliberate (severity, then dependencies, then manifest attributability). Do not
reorder it. Do not work on more than one spec at a time.

  1. docs/specs/0084-concurrency-and-try-defects.md
  2. docs/specs/0086-project-configuration.md
  3. docs/specs/0085-fiber-stack-sizing.md
  4. docs/specs/0081-module-typed-collection-elements.md
  5. docs/specs/0082-demand-driven-component-dependencies.md
  6. docs/specs/0083-text-and-collection-surface.md

PROCEDURE — repeat for each spec, in full, before starting the next:
  a. Read the spec.
  b. Do the spec's own first step if it names one (see per-spec notes below).
  c. Implement exactly what its Validation section lists.
  d. Run: go build ./... && go vet ./... && go test ./...
  e. Confirm the snippet SHA-256 manifest moved only for snippets the spec names.
  f. Commit with a message naming the spec.
  g. Report: what changed, tests added, manifest entries moved. Then continue.

Do not start step (a) of the next spec until (f) of the current one is done.

SCOPE — the rule that matters most:
- Each spec's Validation section is its complete definition of done. Implement
  exactly those cases, no more.
- Do NOT add tests for behaviour a spec does not name. If you believe a case is
  missing, list it in your report and move on. Do not implement it.
- Do not redesign. Decisions are settled. "Options" sections are recorded
  history, not open choices.

PER-SPEC NOTES:
- 0084: C2 is expected to fall out of the C1 fix. Verify. If the Unknown Error
  survives, report it and continue to the next spec — do not diagnose further.
- 0085: largest item in the batch (signal handler, per-worker TLS, two platform
  paths). Its POSIX construction was corrected; implement what is written now,
  not what an earlier revision described.
- 0081: before writing any fix, probe Dict<M.Point, S.Color> and report the
  result. The spec names the fallback if it fails.
- 0082: re-derive the affected-snippet list yourself. The eight named in the
  spec predate 0081 and will have changed.

REPO RULES:
- The compiler is string-in/string-out: no filesystem access, ever.
- Never invoke gcc/clang. No test uses an external C toolchain.
- Go source is LF. Never "normalize line endings".
- Mirror the nearest existing implementation in structure and naming.

ITERATE with scoped tests, full suite only before committing:
  go test ./compiler/checker      # checker changes
  go test ./compiler/generator    # generator changes

STOP AND ASK if any of these happen:
- A probe contradicts its spec.
- A spec's change would require changing a test the spec does not name.
- Two specs appear to conflict.
Do not improvise past any of these.



Sharing the reviews done by other agents. Consider their points on its merits. be unbiased and try to stick to our language goals. update the spec where you have clarity and confidence. Ask me otherwise, with simple language, code examples, options and recommendations.