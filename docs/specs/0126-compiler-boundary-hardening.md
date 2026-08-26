# RFC 0126: Compiler Boundary Hardening

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: what must hold at the public `compiler.Compile` boundary when its
  inputs are hostile or malformed -- module-path validity, a nesting bound,
  panic containment, and the workbench's own limits
- Depends on: the string-in/string-out compiler surface in `compiler/compile.go`
  and the module rules in `docs/reference.md`
- Coordinates with: ADR 0055 (filesystem and build driver), RFC 0124 (compiler
  fuzzing), and the workbench
- Prior art: the Pixel compiler at `Forge/agents/pixel`, a prior attempt at
  this language, validates module paths and contains panics at its boundary.
  Its mechanisms are adopted here and attributed inline.
- Adds one language rule: which characters may appear in a module path.
  Everything else is implementation containment with no source-visible effect.

## Summary

`compiler.Compile` takes a caller-supplied `map[string]string` of sources and
an entrypoint. The map keys are module paths, and they are currently trusted:
they flow unvalidated into generated `#line` directives, `#include` lines,
header guards, and the names of returned artifacts.

Three defects follow, all reproduced against the current tree. This RFC closes
them at the boundary, before any consumer sees a `CompilationResult`.

## Motivation

### Reproduced: preprocessor injection through a module path

Compiling one source whose module path is

```text
app.hex"
#define HEXAL_OWNED 1
#include <stdio.h>
"x
```

exits successfully with no diagnostic and emits, among others:

```c
#include "modules/app.hex"
#define HEXAL_OWNED 1
#include <stdio.h>
"x.h"
...
#line 1 "app.hex"
#define HEXAL_OWNED 1
#include <stdio.h>
```

The injected text lands in the `#include`, the `#line` directives, and the
header guard. Nothing today compiles that output, so the present severity is
low -- but a bundled C compiler and ADR 0055's build driver both change that,
and a compiler that emits attacker-chosen preprocessor directives on a
successful exit code is not a defensible boundary at any severity.

### Reproduced: path traversal in artifact names

A module path of `../../../etc/passwd.hex` exits successfully and yields
artifacts named `modules/../../../etc/passwd.c` and `.h`. Any consumer that
materializes `result.Files` writes outside its output tree.

### Reproduced: unbounded parser recursion

A source of 100,000 nested parentheses terminates the process with
`fatal error: stack overflow`.

This one deserves emphasis: a Go stack overflow is a **fatal runtime error, not
a panic**. `recover()` cannot catch it, so panic containment does not address
it and no boundary guard can. The only fix is refusing the input before the
recursion happens.

The workbench compounds it. `json.NewDecoder(request.Body).Decode(&input)` runs
with no `MaxBytesReader`, no `LimitReader`, and no read timeout, and the server
binds `:8080` -- every interface -- while its startup log claims
`http://localhost:8080`. A request body of about 100 KB ends the process from
any machine that can reach the port.

### Missing: panic containment

There is no `recover()` anywhere in non-test compiler code. An ordinary panic
in any pass escapes `Compile`.

`net/http` recovers handler panics per connection, so the workbench survives
one -- but the caller receives a dropped connection instead of a diagnostic,
and every non-HTTP embedder (ADR 0055's driver, a CLI, a future language
server) takes a hard crash. RFC 0124 asserts "no panic" as a fuzzing invariant,
which is detection. This is containment, and the two are complementary rather
than alternatives.

## What is already sound, and must stay that way

Two existing choices remove whole classes of this defect. They are recorded
here so a later change does not trade them away for readability.

- **String literals lower to numeric byte arrays**, not C string literals:
  `const uint8_t hex_lit_0_bytes[6] = { 104, 101, 0 };`. No quoting, escaping,
  octal-versus-hex ambiguity, or encoding question arises. The prior Pixel
  compiler emitted C string literals and needed a hand-written escaper whose
  correctness turns on a subtle detail -- non-printable bytes must use
  three-digit octal, because a `\x` escape has no length limit and swallows a
  following hex digit. Hexal cannot have that bug. Do not reintroduce C string
  literals for the sake of more readable output.
- **User modules are namespaced under `modules/`**, so a source named
  `hexal.hex` or `main.hex` cannot collide with a runtime artifact. Pixel emits
  its entrypoint at the top level and therefore needs a reserved-stem rule
  rejecting any source file named `main`. Hexal needs no such rule, and should
  not acquire one.

## Decision

### 1. Module paths are validated by allowlist

A module path is legal only if, after removing a single trailing `.hex`
extension, it is non-empty, contains only ASCII letters, digits, `_`, and `/`,
and does not begin with a digit.

Allowlist, not blocklist. A blocklist over `..`, quotes, newlines, and
backslashes invites the next unlisted character; an allowlist is closed by
construction and its diagnostic can state the whole rule. This is Pixel's
`analysis.ValidateModulePath` and it is adopted as-is.

Validation happens once per reachable module, before lexing, and rejects the
compilation. The diagnostic names the offending path and states the rule.

This is the one part of this RFC with a source-visible effect, so it is the one
part `docs/reference.md` records.

### 2. Nesting is bounded

The parser enforces a maximum expression and type nesting depth and reports
exceeding it as an ordinary diagnostic. The bound is a compiler limit, not a
language rule: a program that reaches it is rejected with a clear message
rather than terminating the process.

The limit's value is chosen so that no plausible program reaches it and no
input can exhaust the goroutine stack, and it is stated in one place rather
than duplicated per production.

### 3. The public boundary contains panics

`Compile` recovers, converting a panic into a failed `CompilationResult`
carrying one Unknown Error diagnostic and zero artifacts, so it obeys the same
fail-closed contract every other failure does.

A recovered panic is a compiler defect, never a user error. It is reported as
such, and RFC 0124's fuzzing continues to assert that it never happens rather
than relying on containment to hide it. Containment bounds the blast radius; it
does not lower the bar.

Recovery does not catch stack overflow, which is why rule 2 exists separately.

### 4. The workbench bounds its own input

- Bind loopback by default. The flag may widen it, but the default must match
  what the startup log claims.
- Wrap the request body in `http.MaxBytesReader` with a stated limit.
- Set read, write, and idle timeouts on the server.

These are the workbench's obligations, not the compiler's, and are included
here because they share one root cause and one fix window.

## Non-goals

- A permission, sandbox, or capability model for the compiler.
- Validating source *content*; that is what the checker already does.
- Filesystem access of any kind in the compiler. It stays string-in,
  string-out; ADR 0055 owns materialization.
- Authentication or authorization for the workbench.
- A configurable nesting limit, or exposing the limit as a `Project` setting.
- Making the compiler resistant to memory exhaustion generally. Rule 2 closes
  the reproduced stack case, not every resource question.

## Interaction with ADR 0055

The build driver materializes `result.Files` to disk. It must be able to treat
those paths as already safe, which requires validation to live here rather than
there.

ADR 0055 should still refuse to write outside its output root -- defense in
depth is correct for a component that touches a filesystem -- but that check is
a backstop, not the fix. A driver-side check alone leaves every other consumer
of `CompilationResult` exposed, and there will be more of them.

## Validation

This section is exhaustive. RFC 0126 is complete only when every item passes:

- A module path containing `"`, a newline, a backslash, `.`, `..`, or any
  character outside the allowlist is rejected with a diagnostic naming the path
  and the rule.
- `../../../etc/passwd.hex` is rejected, and no artifact name produced by any
  accepted compilation contains `..` as a path segment.
- The injection reproduction above is rejected rather than compiled.
- A legal module path with `/` separators, `_`, digits after the first
  character, and mixed case still compiles, and the existing module snippets
  are unaffected.
- A path that is empty after removing `.hex`, or that starts with a digit, is
  rejected.
- Nesting beyond the limit produces an ordinary diagnostic; 100,000 nested
  parentheses no longer terminates the process.
- A program at the limit compiles; a program one level past it is rejected.
- A panic injected into a compiler pass produces a failed result with one
  Unknown Error and zero artifacts, and does not escape `Compile`.
- Panic containment does not swallow ordinary diagnostics: a normal rejection
  still reports its own diagnostics unchanged.
- The workbench binds loopback by default and its startup log matches the bind.
- A request body over the workbench's limit is refused with a 4xx status rather
  than read.
- Every guard above is paired with a test proving it fires, per RFC 0124's
  funnel contract.
- Ordinary tests remain pure Go.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` once to state the
module-path rule: the legal character set, the no-leading-digit rule, and that
a violation is a compile-time rejection.

Record nothing else there. The nesting bound is a compiler limit, panic
containment is an implementation guarantee, and the workbench is not part of
the language.
