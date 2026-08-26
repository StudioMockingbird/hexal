# RFC 0126: Compiler Boundary Hardening

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
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
- Adds one language rule: the complete logical source-key grammar. Everything
  else is implementation containment with no source-visible effect.

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

The workbench compounds the exposure by binding `:8080` -- every interface --
while its startup log claims `http://localhost:8080`. It is a temporary local
debug component, so this RFC corrects that misleading exposure without
designing a production HTTP service around it.

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

A source-map logical key is legal only when it:

- is relative and ends in exactly one `.hex` extension;
- uses `/` as its only separator;
- contains one or more non-empty path components before the extension; and
- each component is a Hexal identifier: an ASCII letter first, then ASCII
  letters, digits, or `_`.

`app.hex` and `graphics/shapes_2.hex` are legal. `/app.hex`, `app`,
`a//b.hex`, `a/1b.hex`, `a/../b.hex`, `my-module.hex`, and
`foo.bar/baz.hex` are rejected. This deliberately matches the reference's
identifier-component import grammar rather than accepting arbitrary host
filesystem names; logical keys are compiler identities, not host paths.

Allowlist, not blocklist. A blocklist over `..`, quotes, newlines, and
backslashes invites the next unlisted character; an allowlist is closed by
construction and its diagnostic can state the whole rule. This is Pixel's
`analysis.ValidateModulePath` and it is adopted as-is.

Only reachable keys are validated, preserving the rule that unreachable
source-map entries are ignored. The entrypoint key is validated before its
source is lexed. Each import literal is checked by the existing relative-path
grammar and above-root rejection; once it resolves to a supplied logical key,
that key is validated immediately before its source is lexed. A violation is a
Module Error naming the offending key and the complete rule.

This is the one part of this RFC with a source-visible effect, so it is the one
part `docs/reference.md` records.

### 2. Nesting is bounded

The parser enforces one maximum recursive-syntax depth across every recursively
entered production, including expressions, type expressions, aggregate
literals, patterns, and nested statement blocks. Counting only parentheses or
types leaves equivalent `if`/`match`/literal recursion able to exhaust the Go
stack. Exceeding the bound reports one Syntax Error at the token that would
enter the first disallowed level. The bound is a compiler limit, not a language
rule: a program that reaches it is rejected clearly rather than terminating the
process.

The maximum recursive-syntax depth is 128. This remains far beyond reasonable
handwritten nesting while leaving substantially more stack headroom than 256.
The value is one compiler-owned constant rather than duplicated per
production. Because every checker and generator tree originates from an
accepted parse tree, this parser bound also transitively bounds their structural
recursion; they do not add duplicate depth limits.

### 3. The public boundary contains panics

`Compile` recovers, discards any partial stage state, and returns a fresh failed
`CompilationResult` carrying exactly one Unknown Error diagnostic, a non-nil
empty `Files` map, and finalized project-level statistics. The diagnostic uses
a fixed compiler-defect message; it never exposes the panic value, Go stack,
or host paths. Ordinary returned diagnostics never pass through this recovery
path and remain unchanged.

A recovered panic is a compiler defect, never a user error. It is reported as
such, and RFC 0124's fuzzing continues to assert that it never happens rather
than relying on containment to hide it. Containment bounds the blast radius; it
does not lower the bar.

Recovery does not catch stack overflow, which is why rule 2 exists separately.

### 4. The workbench is local-only

The temporary debug workbench binds `127.0.0.1:8080` directly, matching its
startup log. It gains no public bind flag, request-size policy, timeout policy,
or production-server abstraction. If the workbench ever becomes a shipped
service, its complete HTTP boundary requires a separate design.

## Required sweep

- Keep one module-key validator and call it immediately before lexing each
  reachable logical key; do not duplicate path rules in the generator.
- Preserve the existing relative import resolution, above-root rejection,
  canonical-identity ambiguity check, and unreachable-source behavior.
- Apply the recursion budget at the parser's recursive entry boundary rather
  than scattering unrelated constants through individual productions.
- Add no public panic-injection hook. The recovery wrapper is tested through a
  private function seam in package `compiler`.
- Keep workbench networking out of the core compiler. The compiler remains a
  pure in-memory transformation and gains no source-size, HTTP, or filesystem
  policy.

## Implementation plan

### Phase 0: baseline

1. Record the green test/vet baseline and current snippet manifest.
2. Preserve focused reproductions for preprocessor injection, artifact
   traversal, parser stack exhaustion, escaped panic, and non-loopback binding.

### Phase 1: logical-key boundary

1. Add one validator implementing the exact component grammar above.
2. Validate the entrypoint and each newly reached source key immediately before
   lexing it; leave unreachable entries untouched.
3. Return a deterministic Module Error naming the key and rule, with no
   artifacts and no partial statistics from later stages.
4. Add accepted/rejected key tests, including empty components, extension,
   leading separator, leading digit, punctuation, traversal, and canonical-key
   ambiguity.

### Phase 2: parser recursion budget

1. Inventory every mutually recursive parser entry and place one shared budget
   at the boundary that covers all of them.
2. Increment before recursive descent, decrement on every return path, and
   report the fixed Syntax Error at the first disallowed token.
3. Test depth 128 and depth 129 for parentheses, type
   constructors, aggregate literals, and nested blocks; retain the 100,000-
   parenthesis process-survival regression.

### Phase 3: public panic containment

1. Split `Compile` into a public recovery wrapper and an unexported pipeline
   function without changing the public signature.
2. Use a named result or equivalent single exit so recovery always replaces
   partial output with the exact failure result above.
3. Add a private injected-stage test proving the recovery path fires, plus a
   normal-diagnostic test proving ordinary errors are untouched.

### Phase 4: local workbench bind

1. Replace the all-interface listener with the fixed loopback address
   `127.0.0.1:8080`.
2. Test that the listener address and startup log name the same loopback
   endpoint.

### Phase 5: conformance

1. Implement every Validation item below and no additional behavior.
2. Update `docs/reference.md` once for the logical-key rule only, after
   behavior stabilizes.
3. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
4. Rebuild and restart the workbench because its server behavior changes.

## Non-goals

- A permission, sandbox, or capability model for the compiler.
- Validating source *content*; that is what the checker already does.
- Filesystem access of any kind in the compiler. It stays string-in,
  string-out; ADR 0055 owns materialization.
- Authentication or authorization for the workbench.
- Production HTTP hardening, request-size limits, timeouts, or configurable
  bind addresses for the temporary workbench.
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

- A logical key without `.hex`, with more than one terminal `.hex`, with an
  absolute/leading separator, with an empty component, or with any component
  that is not a Hexal identifier is rejected with a Module Error naming the key
  and the rule.
- `../../../etc/passwd.hex` is rejected, and no artifact name produced by any
  accepted compilation contains `..` as a path segment.
- The injection reproduction above is rejected rather than compiled.
- `app.hex`, `graphics/shapes_2.hex`, and mixed-case identifier components
  compile, and the existing module snippets are unaffected.
- Invalid unreachable source-map entries remain ignored and produce no
  diagnostic, artifact, or statistic.
- Nesting beyond the limit in expressions, types, aggregate literals, patterns,
  and statement blocks produces the specified Syntax Error; 100,000 nested
  parentheses no longer terminates the process.
- A program at the limit compiles; a program one level past it is rejected.
- A panic injected through the private compiler seam produces a failed result
  with the fixed Unknown Error, a non-nil empty `Files`, no exposed panic text
  or stack, finalized statistics, and no escape from `Compile`.
- Panic containment does not swallow ordinary diagnostics: a normal rejection
  still reports its own diagnostics unchanged.
- The workbench binds loopback by default and its startup log matches the bind.
- Every guard above has a focused positive and firing test in this RFC; RFC
  0124 consumes these guards but is not required to implement them.
- Ordinary tests remain pure Go.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` once to state the
logical-key rule: required `.hex`, identifier path components, `/` separators,
reachable-only validation, and Module Error rejection.

Record nothing else there. The nesting bound is a compiler limit, panic
containment is an implementation guarantee, and the workbench is not part of
the language.
