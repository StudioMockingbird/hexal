# Hexal Lang

Hexal is a high-level "syntax sugar" language with Lua-like syntax and a C23 compilation target. It aims to be a "better c", with close mapping of c concepts but with some modern niceties like;

- Sum types
- Match expressions
- No function closures
- Methods on objects
- Generics
- in built datastructures like `List<T>`, `Dict<T, U>`
- in built memory allocation primitives like general purpose allocator (sack!), Stash and Pool
- Zig's allocator passing model

## Critical Directives

- **Execution permission.** A request to implement, fix, or update grants
  permission to edit files and canonical docs; do not ask again. Ask only when
  the request was analysis, exploration, a spec, or a plan, or when an
  unresolved design decision would change semantics.
- **In-memory compiler boundary.** The core compiler is string-in/string-out.
  It accepts all Hexal source contents as `map[string]string`, one logical
  entrypoint name, and a `Project` value of build-time settings whose zero value
  selects defaults, and returns all generated C/header contents as strings. It
  must not read, write, discover, validate, or otherwise inspect host files,
  directories, symlinks, or the working directory. Filesystem drivers, project
  discovery, file watching, caching, and incremental compilation are separate
  future layers unless a specification explicitly introduces them.
- **Crisp communication.** Terse, token-minimal, losing no key information. No
  fluff; code samples where they explain better than prose.
- **Clarify ambiguity.** When a request is ambiguous about user-visible
  behavior, ask one focused question before implementing.
- **A spec's Validation section is exhaustive.** It is the complete definition
  of done for that spec — implement exactly those cases and no more. Do not add
  tests for behaviour the spec does not name, and do not extend a spec's scope
  because an adjacent case looks related. If you believe a case is missing, say
  so and stop; adding it is a spec change, which is the author's call. A
  detailed spec is not an invitation to infer more requirements from it.
- **Verify before recording.** A claim about this codebase — from an audit, a
  review, another agent, or your own reading — is a hypothesis until a probe
  confirms it. Write the probe, run it, quote the output. Findings that fail to
  reproduce get recorded as excluded with the evidence, not silently dropped;
  that record is what stops the same wrong claim returning. This applies to
  your own conclusions as forcefully as to someone else's.
- **Scratch files live in `.tmp/`.** Every probe, throwaway script, captured
  output, or experiment goes under `.tmp/` in this repo — never in the system
  temp directory, never loose in a package. Go tooling skips dot-directories,
  so `go test ./...` and `go vet ./...` do not see `.tmp/`; a probe there can
  import `hexal/compiler` and run as its own package
  (`go test ./.tmp/probe`) without ever joining the gate. Delete the scratch
  when the question is answered; `.tmp/` is expected to be empty between tasks.
- **Files are read and edited with tools, not the shell.** Read, edit, and
  search through the harness's own tools; the shell runs programs — `go build`,
  `go test`, `gofmt`, `git` — and reads their output. Never mutate a tracked
  file with `-replace`, `Set-Content`, `Out-File`, `sed -i`, `>`/`>>`, or a
  heredoc; shell reads for orientation (`wc -l`, `head`, `git diff`) are fine.
  An exact-match edit fails loudly when its target is missing or ambiguous,
  while a regex replace succeeds identically whether it matched zero sites or
  forty — this repo lost `parser.go` to a heredoc and shipped self-recursive
  functions three times from regex rewrites. On Windows,
  `(Get-Content -Raw) | Set-Content` also silently rewrites LF as CRLF. For a
  change too broad for exact-match edits, write the transformation as a Go
  program under `.tmp/` and run it — never `sed`.
- **Workbench validation.** Once code implementation is complete, rebuild the
  `hexal` binary and restart the running workbench through `hexal play`
  before handoff. There is no standalone workbench executable.
- **Pushback.** When a request is wrong or a poor fit for Hexal, push back with
  evidence and rationale.
- **Architecture and style.** Align the compiler's architecture and coding
  style with the 'Clox' compiler tutorials in *Crafting Interpreters* by Robert
  Nystrom.
- **Comments (CARE).** Every comment must contribute at least one Contract,
  Architecture, Rationale, or Edge fact that the code, type, or name does not
  already convey; pure narration, provenance, and stale coordinates are
  deleted. Comments are self-contained, present-tense, and never cite an
  internal RFC, ADR, plan, spec number, spec title, or `docs/specs/` path:
  closed specs are historical records, not the language authority. Comments
  contain only ASCII characters. Complex subsystems expose a short narrative
  spine at their entrypoint; safety reasoning sits adjacent to the operation
  it protects. Prefer an accurate name over a comment, and a deletion over a
  restatement of the next line.
- **Line endings.** Go source is LF, everywhere, without exception — `gofmt`
  emits LF unconditionally, so a CRLF `.go` file is by definition unformatted
  and there is no Windows-convention alternative for it. `.gitattributes` pins
  `*.go text eol=lf` so the working tree matches on every platform and
  `gofmt -l` stays silent. Everything else stays `text=auto`: the git index is
  LF, and CRLF in the working tree for Markdown, JSON, and text is correct on
  Windows and invisible to git. Never "normalize line endings" as a cleanup
  task — the index is already correct, so it produces a whole-file diff carrying
  no change.

- **Simplify.** Before adding an implementation, ask:
  1. Does this need to exist? If not, skip it (YAGNI).
  2. Does this already exist in the codebase? Reuse it; do not rewrite it.
  3. Does the standard library provide it? Use that.
  4. Does the native platform provide it? Use that.
  5. Is there an installed dependency that provides it? Use that.
  6. Can it be one line? Keep it to one line.
  7. Only then, implement the minimum that works.

## Language Goals & Philosophy

1. Hexal should be cohesive, coherent, consistent, uniform and overall feel intuitive.
2. There should only be one obvious way to do things.
3. The language surface must remain small and clean.
4. Prioritize simplicity, elegance, expressiveness, and composability.
5. Hexal is a high-level systems programming language. Users should be able to
   do everything with Hexal that they can do with C.
6. Static typing.
7. Compiles to human-readable, formatted C23 with `#line` source mapping back
   to Hexal.
8. Can import C code and headers.
9. Trivial import of C libraries and tools.
10. Incremental compiler. Only recompile the module that changed.
11. Fast compilation.
12. Trivial concurrency and multithreading.
13. Low ceremony: minimize boilerplate and syntactic noise.
14. Promote readability over compactness and complicated instructions.
15. No runtime overhead.
16. No undefined behavior.
17. If it compiles, it runs.
18. Compiler should catch every memory error that a local analysis can decide
    without adding a language concept or disproportionate checker complexity.
19. Leans towards Odin and Zig for feature parity and semantics
20. Doesnt carries heavy ownership and lifetime semantics, in favor of compiler assisted manual memory management.

## Architecture

- Forward-only pipeline with clean separation of concerns.
- **Fail-closed architecture.** Any invalid or unsupported program must fail
  with a diagnostic. If the compiler cannot classify the failure, report an
  `Unknown Error` and make clear that the problem is in the compiler, not the
  user's program.
- Analyzer and code-generation dispatch must handle every supported syntax
  node explicitly.
- **There is no analyzer pass and none is planned.** Checked syntax goes
  directly to code generation. Analysis that needs more than the checked tree —
  constant folding, provenance, freed-state — lives in the checker beside the
  facts it consumes, not in a separate stage. Do not introduce an analyzer
  package; if a future feature genuinely cannot be expressed in the checker,
  that is a spec-level argument, not an implementation decision.
- **Earliest diagnostic ownership.** Define each compilation error at the
  earliest phase that can prove it. If the same source construct could produce
  errors in multiple phases, keep the earliest diagnostic and do not let later
  phases reinterpret or duplicate it. A later phase may report independent
  errors only after it receives valid output from the preceding phase.
- Unsupported syntax must produce a structured diagnostic; never return an
  empty result, emit a placeholder comment, or silently omit output.
- New features mirror the closest existing implementation—structure, naming,
  diagnostics, and test kinds—before inventing new ones.
- Prefer plain loops, switches, and direct data over frameworks and layers of
  indirection. When two solutions differ in complexity, take the simpler one.
- The same standard applies to generated C23: humans maintain and debug it, so
  it should remain as plain as the compiler source.
- The generated C23 must always try to use the latest features released for C, as long as the latest gcc & clang versions support that.
- **Never reinvent a C23 or toolchain facility.** Before writing a generated-C
  helper, a lowering formula, or a portability workaround, check whether the
  standard already provides it: a C23 header (`<stdckdint.h>` for checked
  add/subtract/multiply, `<stdbit.h>` for bit and endianness queries,
  `<stdatomic.h>`, `<threads.h>`), a C23 language feature (`nullptr`,
  `constexpr`, `typeof`, `[[noreturn]]` and the other attributes, one-argument
  `static_assert`), or a builtin both GCC and Clang document. Use it directly;
  do not wrap it in a helper that only delegates. Write a compiler-owned helper
  only when no standard facility implements the required semantics exactly, and
  say in a comment which facility was considered and why it does not fit. This
  rule applies to the generated C, not to the compiler's own Go.
- **Sweep when a contract changes.** A specification that establishes a new
  invariant — a toolchain guarantee, a representation fact, a target
  qualification — must also remove the code that existed only because that
  invariant was previously absent. Removing the assertion without removing the
  defense it justified leaves dead caution behind. Name the swept code in the
  spec, or state explicitly that none exists.

## Target Profiles

Hexal target profiles are compiler-owned keys. A listed profile is a target
commitment, not a claim that the compiler, runtime pack, toolchain, and C23
qualification already support it. `x32_64` means `x86_64`.

POSIX is an operating-system family rather than one target triple. Linux is
the initial POSIX target; the libc is part of the profile because it changes
the ABI, headers, linker inputs, and runtime-pack requirements.

| Family | Hexal target profile | Toolchain target triple | System ABI |
| --- | --- | --- | --- |
| Windows | `x86_64-windows-gnu-ucrt` | `x86_64-w64-windows-gnu` (Zig: `x86_64-windows-gnu`) | MinGW-w64 + UCRT |
| Windows | `aarch64-windows-gnu-ucrt` | `aarch64-w64-windows-gnu` (Zig: `aarch64-windows-gnu`) | MinGW-w64 + UCRT |
| POSIX/Linux | `x86_64-linux-gnu` | `x86_64-linux-gnu` | glibc |
| POSIX/Linux | `aarch64-linux-gnu` | `aarch64-linux-gnu` | glibc |
| POSIX/Linux | `x86_64-linux-musl` | `x86_64-linux-musl` | musl |
| POSIX/Linux | `aarch64-linux-musl` | `aarch64-linux-musl` | musl |
| POSIX/Linux | `riscv64-linux-gnu-rv64gc` | `riscv64-unknown-linux-gnu` | glibc + RV64GC/LP64D |
| POSIX/Linux | `riscv64-linux-musl-rv64gc` | `riscv64-unknown-linux-musl` | musl + RV64GC/LP64D |
| macOS | `x86_64-macos` | `x86_64-apple-darwin` | Apple libc + system SDK |
| macOS | `aarch64-macos` | `aarch64-apple-darwin` | Apple libc + system SDK |

Every target-specific runtime pack and prebuilt native dependency is keyed by
the exact Hexal target profile. macOS profiles also require an explicit
deployment target and SDK qualification; those values are part of the build
record, not an implied universal default.

The initial RISC-V profiles fix the `rv64gc` instruction set and `lp64d` ABI;
the compiler must not inherit these choices from the host toolchain.

The matrix covers the major desktop and server targets. If POSIX is intended
to include non-Linux Unix systems, the main future omission is FreeBSD:
`x86_64-unknown-freebsd` and `aarch64-unknown-freebsd`. Android and iOS are
separate mobile targets, not implied by the profiles above, and should be
added only with their own SDK/sysroot, runtime pack, and C23 qualification.

## Documentation

Keep these two canonical documents updated once per feature, after behavior
stabilizes—not repeatedly while it is still changing:

1. `docs/reference.md` — sole normative syntax and semantic contract; formal
   EBNF appears first so syntax and semantics change together.
2. `docs/status.md` — open TODOs and open bugs only, each naming its owning
   spec; it does not define semantics and does not record completed work. A
   spec's `Status:` header records what is done. Delete an entry when it
   closes; add one only with a spec to point at.

**Reference synchronization.** Every spec implementation must review
`docs/reference.md` for affected grammar, semantics, signatures, restrictions,
and C23 contracts. Apply every required update after behavior stabilizes and
before marking the spec implemented or closed. If no edit is required,
explicitly verify that the implemented behavior already matches the reference.
An implementation is incomplete while code, tests, and `docs/reference.md`
disagree.

These two are the only canonical prose documents. `docs/language.md` was retired
after its still-relevant content was migrated into `docs/reference.md`; do not
recreate it or add a third prose document alongside these.

The normative grammar is `GRAMMAR.ebnf` at the repository root, in
`golang.org/x/exp/ebnf` form, which `docs/reference.md` links as authoritative
and `TestGrammarIsVerifiable` parses and verifies. It is machine-checkable data
rather than prose, which is why it lives outside the two documents; the older
`docs/grammar.ebnf` it replaced is gone. A change to accepted syntax updates the
grammar in the same change as the parser.

`docs/reference.md` is primarily an input to agentic development workflows and
secondarily a lookup document for humans. Optimize it for precise retrieval:

- Include only current syntax rules and semantic contracts.
- Exclude tutorials, walkthroughs, historical narrative, and illustrative
  examples.
- Express information as general rules, exact signatures, tables, acceptance
  conditions, or rejection conditions. Replace any proposed example with the
  rule it demonstrates.
- Keep each rule in one authoritative location. Prefer explicit, dense wording
  over explanatory prose while preserving every semantic edge case.

Closed specs are historical records, superseded wherever they disagree with
`docs/reference.md`. Some contain syntax the language never had — blanket `:=`
inference in older proposals; do not reintroduce it. `:=` is not a declaration
operator in any form and the compiler rejects it outright: every value binding
is introduced by `let`, which states its type exactly once, on one side or the
other. `let name: T = initializer` states it on the left; `let name =
initializer` lets the initializer state it, and is rejected when the initializer
is contextual — an integer, float, or string literal, `nil`, an array literal,
or a `match` whose every arm is contextual. Stating it on neither side is an
error. `mut` follows `let` when the binding is replaceable. Other closed specs
predate RFC 0061 and show the old delimiter-free forms (`fun f()` or
`if cond` without the mandatory `do`/`then`); the language now requires those
block openers, so treat their absence in a closed spec as superseded, not as
authority. Closed specs before RFC 0142 also show `type Name = Target` and
`type Name as ... end` declarations, `Name { field = value }` brace
construction (including ADT variant construction), bare unit ADT variant
values (`Owner.Variant` with no call), `.new()` compiler-owned constructors,
and `impl Receiver.name(...)` method declarations — all historical. The
current forms are `type Name is ...`, call-shaped `Name(field = value)`
construction for structs and ADT variants (`Owner.Variant(field = value)`,
always called even for unit variants), `Type(...)` for every compiler-owned
canonical constructor (Heap, Stash, Pool, List, Dict, Channel, Mutex, Atomic,
Error), and `method Receiver.name(...)`. Do not copy a rule out of a spec
without checking it against `reference.md` first.

## Testing

- Keep unit tests light. Add only the focused coverage fundamentally required
  to validate an individual compiler stage or helper.
- Active integration tests live in `compiler/tests/integration/`, one file per
  language facet, named for the facet (`pointers_test.go`,
  `operators_test.go`). They are package `integration`, import `hexal/compiler`,
  and exercise only its exported API. Never name a test file after a spec, and
  never put a spec number in a test function name or comment — state the
  behavior or edge condition the test protects; provenance belongs in git and
  the spec archive. Together these files must verify the public compiler
  behavior end to end.
- External C23 validation lives in `compiler/tests/c23validation/`
  (`package c23validation`), gated by `//go:build c23`. Closed RFC 0125 owns it
  and landed it: the package is live, not dormant. A tagged run resolves a C
  toolchain, compiles generated C under it, executes the result, and asserts on
  stdout, exit status, and trap text. Treat a tagged run as something that
  starts external processes and takes real time, never as a type-check.
  Lower-camel functions there are shared helpers (`buildGeneratedC`,
  `runGeneratedC`, `trapGeneratedC`, `assertCompiles`), not disabled tests.
- Ordinary tests never invoke an external tool — gcc, clang, or anything else.
  All ordinary tests are pure Go.
- **A green ordinary suite does not mean the generated C is correct.** No
  ordinary test compiles or executes generated C, so `go test ./...` and
  `go vet ./...` both pass on output that a C compiler would reject, and on
  output that compiles and behaves wrongly. That is a property of the ordinary
  suite by design, not a gap waiting to be closed: ordinary tests stay pure Go
  and toolchain-free.

  The tagged `c23` lane is where generated C is actually compiled and run, so a
  claim that the generated C works is backed by a tagged run or by nothing.
  `go vet -tags c23` only type-checks the package — it is not that evidence.
  Known instances of the gap are recorded under "Known coverage gaps" in
  `docs/status.md`.

  What follows for anything touching the generator: assert on the **text** of
  the emitted C — that a required declaration precedes its use, that a helper is
  emitted once, that an include is present or absent. A test that only checks
  the compiler returned success proves nothing about the artifact.
- **The snippet manifest is the generator's regression net.**
  `workbench/snippets/testdata/generated-c-sha256.json` holds a SHA-256 per
  generated artifact for every catalog snippet;
  `TestCatalogProgramsCompile` recompiles them all and fails naming the exact
  snippet and file. Treat a failure as a real finding until you have shown
  otherwise.

  Rebuild the baseline only for a change that legitimately alters generated
  output, never to make a failure go away, and never by hand-editing hashes or
  weakening the assertion. To rebuild: write a temporary test in package
  `snippets_test` that walks `snippets.Load()`, compiles each snippet with
  `compiler.Compile(snippet.Sources, snippet.Entrypoint, compiler.Project{})`,
  SHA-256s every entry of `result.Files`, and writes
  `json.MarshalIndent(manifest, "", "  ")` plus a trailing newline to that
  path. The manifest nests category, then snippet, then artifact. Delete the
  temporary test afterwards.

  Then review what moved, and say so in the commit message. The artifact
  breakdown comes straight from the diff:

  ```
  git diff workbench/snippets/testdata/generated-c-sha256.json |
      grep '^+' | grep -oE '"[^"]+\.(c|h)"' | sort | uniq -c
  ```

  A change that claims to touch one family and moves another's artifact has
  either a wider blast radius than its spec says or a defect. Find out which
  before committing.
- `go test ./compiler` does not run the full-pipeline suite (that package
  declares only the module-graph tests in `modulegraph_test.go`); use
  `go test ./...` or target `./compiler/tests/integration`.
- The benchmark suite and the complexity report live in
  `compiler/tests/benchmarks/`, every file a `_test.go` file so that
  `go build ./compiler/...` compiles none of the third-party complexity
  libraries. Run them with:
  - `go test -bench . -benchmem -benchtime 1x ./compiler/tests/benchmarks`
  - `go test -run TestComplexityReport -v ./compiler/tests/benchmarks`
  - `go test -tags benchmetrics -bench Traversal -benchtime 1x ./compiler/tests/benchmarks`
    for walk and node counts, which exist only under that tag.
- `go test ./...` must pass with no external toolchain installed.
- Future test packages require a genuinely distinct execution lifecycle,
  dependency boundary, or toolchain requirement; a Go directory is a package
  boundary, not a visual grouping mechanism.
- Intentional overlap between unit and integration tests is expected: unit
  tests isolate stage behavior, while integration tests confirm that the same
  behavior survives the complete compilation pipeline.

## Spec format

All specs live in `docs/specs/`, one flat number sequence, `NNNN-kebab-name.md`.
Numbers are permanent identifiers: never renumber, never reuse. Each spec names
its kind in the header:

- Feature Specifications: Rust-Style RFC
- Language Semantics: ISO/IEC Language Standard Format
- Architecture Decisions: ADR (Architecture Decision Record)
- Execution Plans: named `...-plan.md`, header links the spec they implement

A spec's `Status:` header is the only completion record while it is active. A
terminal status (Closed, Discarded, Superseded, or Rejected) means every
current-behavior claim the spec makes has been verified against
`docs/reference.md` and the tree; the spec then moves to `docs/specs/archived/`
in that same change, unchanged from that point on — a Spec once closed is
immutable, so do not edit an archived file even when the feature it describes
is later updated. Numbers are permanent identifiers: never renumbered, never
reused, and a new spec always takes a number greater than every number ever
assigned. An active spec should prefer citing the current rule in
`docs/reference.md` over a closed spec for anything load-bearing, since the
reference is authoritative and the archive is historical.
