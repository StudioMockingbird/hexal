# RFC 0086: Project Configuration

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-19. `compiler.Project` carries
  `TaskStackReserve` and `TaskStackCommit`, validated before any stage runs:
  commit at most reserve, both page multiples when non-zero, reserve non-zero
  after defaulting. Defaults live once in `compiler/generator` as
  `DefaultTaskStackReserve` (1 MiB) and `DefaultTaskStackCommit` (8 KiB);
  zero renders the historical "1u << 20" and `CreateFiberEx(0, 0, ...)`
  spellings, so a zero `Project` keeps every artifact byte-identical. All
  callers take the third argument explicitly; the manifest is unchanged.
- Created: 2026-08-19
- Scope: one struct carrying build-time settings into `compiler.Compile`, and
  the rule for what may live in it
- Depends on: nothing. RFC 0085 is its first consumer and can land before it
  using constants with the same defaults.
- Coordinates with: `AGENTS.md` (in-memory compiler boundary), RFC 0055
  (filesystem and build driver — owns reading a file into this struct),
  RFC 0052 (target profiles), `docs/reference.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, or the string-in/
  string-out property of the compiler

## Summary

Settings that are neither language semantics nor source code are currently
literals inside runtime templates. RFC 0085 adds two more (Task stack reserve
and initial commit).

Introduce one `Project` struct passed to `compiler.Compile`, holding
build-time settings with working defaults. It is a Go value, not a file: reading
a file into it belongs to the build driver, and this RFC deliberately stops
short of that so the compiler stays free of filesystem access.

## Why now

Two forces meet:

- RFC 0085 needs two numbers that a caller may legitimately want to change, and
  hard-coding them in a template makes them unchangeable without editing the
  compiler.
- `AGENTS.md`'s boundary rule is precise about what `Compile` accepts — "all
  Hexal source contents as `map[string]string` plus one logical entrypoint
  name". Adding a third parameter changes a documented contract, so it should
  happen once, deliberately, with a stated rule for what may be added later —
  not incrementally as each setting appears.

## The struct

```go
// Project carries build-time settings that are not part of the language.
// The zero value is valid and selects every default, so Compile(sources,
// entrypoint, Project{}) behaves exactly as the two-argument form did.
type Project struct {
    // TaskStackReserve is the per-Task address-space ceiling in bytes.
    // Zero selects 1 MiB. RFC 0085.
    TaskStackReserve uint64

    // TaskStackCommit is the bytes committed when a Task is spawned.
    // Zero selects 8 KiB. RFC 0085.
    TaskStackCommit uint64
}

func Compile(sources map[string]string, entrypoint string, project Project) CompilationResult
```

**The zero value is the contract.** Every field means "default" when zero, so
adding a field never changes an existing caller's behaviour, and no caller has
to enumerate settings it does not care about.

## What may live here

A setting belongs in `Project` when all four hold:

1. It is **not** language semantics. Anything a Hexal program can observe by
   type-checking belongs in `reference.md`, not here.
2. It changes generated C or generated-runtime behaviour.
3. A reasonable project might want a different value.
4. It has a working default, so the zero value stays valid.

Explicitly **not** eligible: anything that makes the same source mean different
things. A flag that changed whether a program type-checks would fork the
language, which `AGENTS.md` goals 1–3 exclude.

Candidates for later, listed so the shape is clear and **not** included now:
worker-thread count (currently logical processors), target profile (RFC 0052),
and diagnostic verbosity. Each needs its own justification; none is added
speculatively.

## Validation of values

`Compile` validates `Project` before any stage runs and returns a normal
diagnostic on failure — this is a compiler input like any other, and invalid
input fails closed:

- `TaskStackCommit` must be less than or equal to `TaskStackReserve`.
- Both must be multiples of 4096 when non-zero.
- `TaskStackReserve` must be non-zero after defaulting.

4096 is the page size the POSIX `mprotect` guard depends on. Windows'
64 KiB allocation granularity does not need a rule here: commit is
demand-driven on that platform, and `CreateFiberEx` rounds its own arguments.
Per RFC 0085, `TaskStackCommit` is inert on POSIX and meaningful only on
Windows — the validation applies uniformly regardless, so a value is never
silently accepted on one target and rejected on the other.

Diagnostic category is `Configuration Error`, and the message names the field
and the constraint it violated.

## What this RFC does not do

**It does not read a file.** The compiler performs no filesystem access, and
that rule is unchanged. `Project` is populated by the caller — today the
workbench and tests, later a build driver.

A `hexal.json` or similar is the natural next step, and it belongs to RFC 0055,
which already owns the host layer. This RFC's contribution to that future is the
struct it will deserialize into, so the format question can be answered
independently and later.

## Documentation changes

`AGENTS.md`'s boundary rule gains the third input:

> It accepts all Hexal source contents as `map[string]string`, one logical
> entrypoint name, and a `Project` value of build-time settings whose zero value
> selects defaults, and returns all generated C/header contents as strings.

The prohibition on filesystem access is untouched and, if anything, load-bearing
here: `Project` is the reason a caller no longer needs the compiler to read
configuration itself.

## Invariants

1. `Compile(sources, entrypoint, Project{})` produces byte-identical output to
   today's `Compile(sources, entrypoint)` for every program.
2. The compiler performs no filesystem access. `Project` is a caller-supplied
   value; nothing in it is a host path.
3. No field changes whether a program type-checks. Configuration cannot fork the
   language.
4. Every field has a documented default and a zero value meaning "default".
5. Invalid configuration produces a diagnostic, never a panic or a silently
   clamped value.

## Validation

- The whole snippet catalog compiles with `Project{}` and the manifest is
  **unchanged** — this RFC alone changes no output.
- A non-default `TaskStackReserve` reaches the generated runtime and appears in
  the emitted C.
- Each validation rule has a rejecting test with its expected message.
- `CompilationResult`, `CompilationStats`, and artifact naming are untouched.
- Every existing caller is updated: `workbench`, `compiler/tests/integration`,
  `compiler/tests/c23validation`. A compile error at any call site is the point
  — the contract changed and callers should say so explicitly rather than
  inherit a default silently through an overload.
- `go test ./...`, `go vet ./...`.

## Non-goals

- A configuration file, its format, discovery, or precedence — RFC 0055.
- Per-module or per-target configuration. One value applies to the whole
  compilation.
- Runtime configuration. Everything here is fixed at build time.
- Environment variables or command-line parsing. Those are driver concerns.
- Adding fields beyond RFC 0085's two. The eligibility rule exists so later
  additions are argued, not assumed.

## Drawbacks

- It changes a documented public contract for two numbers. The justification is
  that it changes it **once**, with a rule for growth, rather than once per
  setting — and RFC 0085 is the second setting already.
- A settings struct invites accretion. The four-part eligibility rule and the
  "cannot fork the language" invariant exist to make each addition arguable;
  enforce them in review.
- Keeping the struct in the compiler package while the file format lives in a
  future driver means the two land separately, and the struct will look
  over-built until the driver exists.
