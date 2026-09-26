# RFC 0243: Central Diagnostics and Stable Keys

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented and validated 2026-09-26
- Updated: 2026-09-26
- Created: 2026-09-24
- Origin: RFC 0241's finding T1 and the requirement to move all compiler
  diagnostic wording into one package. Deferred RFC 0242 names
  stable diagnostic identity as a prerequisite for an editor protocol while
  explicitly not authorizing it — its "Does not authorize" line covers "stable
  diagnostic-code migration", and its open questions ask how identities are
  introduced without breaking `CompilationResult.Stderr`. Nothing owned the
  work in between
- Depends on: nothing. Every mechanism considered here extends code that ships
- Coordinates with: archived RFC 0229 (whose local-wording decision this RFC
  supersedes for diagnostics only), archived RFC 0238 (declarative fact
  guards), deferred RFC 0242 (language tooling), and RFC 0241 (the review)
- Reference synchronization: the rendered diagnostic format is user-visible,
  so implementation must update `docs/reference.md` after behavior stabilizes

## The problem

A Hexal diagnostic has no identity. `compiler/types/diagnostic.go` renders

```go
"[" + string(diagnostic.Category) + "] " + diagnostic.Message + location
```

and `Message` is a string composed at the emitting site:

```go
diagnostics = append(diagnostics, nameErrorAt(declaration.Name,
    declaration.Name.Lexeme+" is a protected built-in name"))
```

The only handle anything has on a diagnostic is its rendered English. RFC
0241's review recorded 1,423 emission sites and roughly 1,192 distinct
message literals in `compiler/checker` at its 2026-09-24 snapshot. It did not
preserve a counting method, so these figures are historical scale indicators,
not current migration targets. Phase 0 of this RFC inventories the current
sites with a stated selection and deduplication method against its own
starting revision.

The constructors are concentrated, but the semantic sites are not. A
production-source search for `Diagnostic{` and `NewDiagnostic(` finds 15 Go
files under `compiler/`; a search for the five shared checker helpers
`typeErrorAt`, `nameErrorAt`, `moduleErrorAt`, `semanticErrorAt`, and `unknownAt`
finds 683 matching lines, including the helper declarations. Parser helpers
also accept free-form messages. Adding one field at the constructors cannot
assign meaningful identities to all those calls: the migration must classify
the conditions at their emitting sites.

The 2026-09-25 production-file probes returned:

```text
rg -l 'Diagnostic\{|NewDiagnostic\(' compiler --glob '*.go' --glob '!**/*_test.go' | Measure-Object -Line
  Lines: 15
rg -n 'typeErrorAt\(|nameErrorAt\(|moduleErrorAt\(|semanticErrorAt\(|unknownAt\(' compiler/checker --glob '*.go' --glob '!**/*_test.go' | Measure-Object -Line
  Lines: 683
```

These are inventory hints, not a count of distinct diagnostic conditions.

Three costs follow, and all three are live rather than hypothetical.

**Rewording a message silently breaks every consumer.** Tests, specs, and
documentation quote message text because there is nothing else to quote. During
the RFC 0163 and RFC 0165 work three separate specs were found quoting
diagnostics that had since been reworded — `use rune_cursor()` had become
`use bytes() for indexed byte access` after RFC 0224, and a `.value`-era null
message had become a dereference message after RFC 0161. Each was a silent
documentation defect that only a manual re-probe caught. An identity would have
made those references stable across the rewording.

**A user cannot name a diagnostic.** There is no stable handle to search for
or report without pasting a sentence. A key supplies that handle;
suppression would be a separate language decision and is not a benefit this
RFC promises. `TS2345` is a thing a person can look up; `[Type Error] this
pointer's storage was released on every path to this point` is prose.

**The diagnostic surface cannot be enumerated.** There is no list of what the
compiler can report, so no guard can check one. Archived RFC 0238 established
the pattern that closes exactly this class of gap — assert that every spelling
used in code corresponds to a declared record and vice versa — and it cannot
reach diagnostics, because there is nothing declared to compare against. Every
other comparable surface in the compiler now has such a guard; diagnostics are
the largest one that does not.

## The central wording contract

The author's goal is stronger than identity alone: **all compiler-owned
diagnostic wording moves out of lexer, parser, checker, generator, and compile
code into one central package**. An emitting phase decides *which condition*
occurred and supplies contextual values and a source location; it does not
assemble the user-facing sentence, punctuation, category prefix, or location
suffix. Both the identity and complete rendered message have one owner.

This explicitly supersedes archived RFC 0229's Decision 7 for diagnostics.
That decision kept wording beside emitters; it remains historical and is not
edited. The rest of RFC 0229's fact-registry boundaries are unaffected. RFC
0241's identity-only recommendation is likewise historical where it conflicts
with this RFC. Centralization is accepted here because it is the requested
outcome, not justified by localization or by copying TypeScript's generator.

The package may use multiple Go files to stay navigable, but there is one
import path and no message prose left in phase packages. Conditional wording
is selected inside that package from explicit typed inputs. Names, types,
counts, and structured reason values remain data passed from the emitter;
passing `err.Error()` or another preassembled English fragment to evade
central ownership is not allowed. The central package owns diagnostic prose,
not whether a program is valid: the earliest phase still decides that in its
existing code.

## Scope and identity boundary

This RFC covers diagnostics owned by the in-memory `compiler.Compile` pipeline:
configuration and module resolution, lexing, parsing, checking, generation,
and the compiler's own `Unknown Error` fallback. A key identifies a stable
user-correctable condition or one internal-invariant failure family, not a
particular English sentence, source position, variable name, or Go call site.
Parameterized instances of one condition share an identity; different causes
must not share one merely because their messages happen to match. Rewording a
message or moving its emitter between helpers does not renumber the condition.

External C compiler/linker stderr, runtime trap text, and driver-owned
environment or process errors are not `compiler.Compile` diagnostics and do
not receive invented Hexal keys here. If a lower-level Go error reaches
`mergeDiagnostics` without a structured diagnostic, the existing `Unknown
Error` classification remains the fail-closed boundary and receives one
dedicated compiler-internal identity. It must not derive an identity from the
unstructured error text.

Identity and central wording do not alter category, stage, source span, module
attribution, ordering, or earliest diagnostic ownership. The central package
formats messages but never decides whether an error occurs. Tests may use
identity for stable assertions while retaining focused wording assertions
where the exact sentence is a contract.

## Decisions

### 1. Render the identity by default

The key appears in each compiler-owned diagnostic's normal rendered output,
for example `[Name Error name.unknown-variable] unknown variable score at
app.hex:3:7`.
`CompilationResult.Stderr` retains its `[]string` shape, but its diagnostic
strings change once. No rendering option or second output shape is added.
The implementation must migrate exact-message tests and synchronize
`docs/reference.md` after the behavior stabilizes. The generated-C snippet
manifest does not contain diagnostics and must remain byte-identical.

### 2. Use descriptive, permanent keys

Use a lowercase ASCII, dot-qualified descriptive key such as
`name.unknown-variable` or `syntax.expected-value`. The grammar is
`segment ("." segment)+`; each segment starts with a lowercase ASCII letter
and continues with lowercase ASCII letters, digits, or `-`, never ending in
`-`. A key is assigned manually to the condition and is not derived from its
message, current category, source file, or Go identifier. Its namespace is
organizational, not an instruction to rekey a diagnostic if ownership or
wording changes. An old key remains reserved even after its condition is
removed. No parallel numeric code is introduced.

### 3. Migrate the whole compiler surface

"The entire error message" rules out a permanent mixed state. Migration may
land in reviewable phase-sized commits, but the implementation is incomplete
until every compiler-owned diagnostic uses the central wording owner and has
an identity. No old free-form helper remains as a compatibility path. This is
a large diff; the single-package ownership and complete guard are the result
that justifies it.

### 4. Where does the registry live?

Use one compiler-owned diagnostics package for identities, category/stage
records, and all message wording. `compiler/specdata` deliberately stores
facts rather than behavior and is therefore not the home for hundreds of
message-formatting functions. The new package imports no compiler stage or
`compiler/types`, so `compiler/types` and every emitting stage can depend on
it without a cycle. Plain typed Go constructors own the wording in that
package, and the visible identity uses the descriptive-key form above.

### 5. Use one typed Go package

Plain Go functions in `compiler/diagnostics` own keys and complete wording.
For example, `diagnostics.UnknownVariable(name)` returns both the identity and
`"unknown variable " + name`. Go checks each constructor's parameter types;
there is no JSON catalog, generated wrapper layer, or generic
`format(id, ...any)` function. Multiple files may organize the one package
without returning wording ownership to the emitting phases.

### 6. Do related spans come with it?

No. RFC 0242 lists "stable identities, parameter data, primary spans, and zero
or more related spans" as a prerequisite bundle for editor tooling. Hexal
already has primary spans. Related spans, structured parameters, and a public
structured-results API remain separate future tooling work; identity does not
require them or justify a second diagnostic representation now.

### 7. Which errors move to the central package?

Only errors produced while compiling Hexal source through the in-memory
compiler pipeline. Driver failures such as a missing Clang executable or a
linker exit, and C runtime traps, remain owned by their respective layers.
Centralizing those across Go process and generated-C boundaries would be a
separate design, not an incomplete part of this migration.

## Implementation architecture

### One package owns keys and complete wording

Add `compiler/diagnostics`, importing only the Go standard library. It owns a
typed diagnostic ID, the permanent ID/category/stage records, and every
compiler-owned diagnostic sentence. Its public constructors take typed
contextual values and return an immutable message value carrying the ID and
complete English text. The value's fields are unexported, so callers cannot
manufacture a message with an arbitrary ID or phase-local prose. For example,
the checker asks for
`diagnostics.UnknownVariable(name)`; it does not concatenate `"unknown
variable " + name`. Central constructors may share private formatting helpers,
but the package must not become a generic rule engine for deciding whether a
program is valid.

Use plain typed Go functions in the package, not a JSON generator or a
`format(id, ...any)` API. Static sentences can be constants behind functions;
dynamic sentences are built by typed functions. The package may have separate
files for lexical, syntactic, type, module, and internal-error wording while
remaining one import path. The code never derives an ID or Go name from
English text. Its `Validate` check rejects empty or repeated identities,
unknown categories or stages, and incomplete records. Removed identities
remain reserved and are never reused for a different condition.

The current stage set is `compile`, `lexer`, `parser`, `checker`, and
`generator`; the category set is the existing `ErrorCategory` set. Move the
category identity to the central package and keep `compiler/types` aliases as
needed for source compatibility; do not maintain two independent category
tables. Introducing a new stage or category requires an explicit
registry-contract change rather than an arbitrary string at a site.

`compiler/types.Diagnostic` gains the typed identity. Its `Error()` method
delegates the complete bracket, message, and location rendering to
`compiler/diagnostics`; it does not assemble a prefix or suffix itself. A
centralized constructor
accepts a `diagnostics.Message` and a source location, deriving category,
stage, ID, and complete wording from that value and its record. The emitter
still owns the source location and error decision. The production call sites
of `Diagnostic{` and `NewDiagnostic(` migrate to this constructor or a
phase-local source-location adapter around it. Parser helpers that currently
accept `message string` instead accept `diagnostics.Message`. The checker's
category-named helpers (`typeErrorAt`, `nameErrorAt`, and their siblings)
collapse to one token-location adapter: category comes from the central
message record, not from a second choice at the checker site. This prevents
a Type Error helper from accidentally receiving a Name Error message and
leaves no raw-string helper through which to smuggle local prose.

An unstructured Go error at a compiler boundary receives a dedicated central
`Unknown Error` message; it is not classified or displayed by matching or
copying its raw English text. The panic-recovery diagnostic has its own
internal identity and still hides the panic value and host path. Every failed
`compiler.Compile` result therefore contains key-bearing, centrally worded
Hexal diagnostics. A malformed or unknown identity is a compiler defect,
never a reason to emit a key-less user error silently.

Existing user-correctable failures that happen to be carried by plain Go
errors are not internal defects. For example, `validateLogicalKey` currently
returns `fmt.Errorf` text that `Compile` displays as a Module Error. Replace
that boundary with typed reason data and a central Module Error constructor,
preserving its current sentence. Reserve the generic Unknown Error fallback
for an unexpected unclassified Go error. `failureResult` and
`compiler/types.ErrorMessages` must not pass such an error's raw `Error()`
text to `CompilationResult.Stderr`. Only this previously unstructured fallback
may intentionally change its old English text; the baseline comparison below
remains exact for every classified source/program rejection.

A `Compile(map[string]string{"bad?.hex": ""}, "bad?.hex", Project{})` probe
returned exit 1 and this diagnostic, confirming that a plain Go validation
error is currently presented as a user-correctable Module Error:

```text
[Module Error] logical key "bad?.hex" is invalid: a logical key must be relative, use "/" as its only separator, end in exactly one ".hex" extension, and have every path component be a Hexal identifier at 1:1
```

### Completeness guard

A test inventories production Go source under `compiler/`, excluding test
files, through Go's syntax tree. It rejects direct diagnostic construction
outside the approved constructor, raw `message string` helpers on a diagnostic
construction path, and direct assignment of a diagnostic's message field
outside the central constructor. It does not ban a string parameter in an
unrelated generated-C runtime helper. The typed constructor and helper
signatures make locally assembled strings unusable as diagnostic messages.
Each public central message constructor names exactly one constant registered
ID in a direct call to the package's private message builder; a conditional
that selects a different diagnostic calls a different named constructor.
This convention lets the guard check the central constructor-to-ID mapping
without whole-program dataflow analysis. It also checks that every active
registry identity has a constructor used by at least one emission site.
Reserved retired IDs need no emitter. The guard reports the missing, orphan,
or locally worded condition and its source location. A separate focused test
verifies that category and stage from each registry record reach the rendered
diagnostic unchanged. Do not satisfy completeness with two manually
maintained copies of the ID list or a text search over message text.

The inventory covers the compiler's ordinary failure path, merge fallback,
and panic recovery as well as the stage packages. External tool stderr and
runtime traps remain outside its source set. No ID is assigned merely to make
one Go source line unique: shared semantic conditions may have multiple
emission sites, and one helper may receive many IDs.

## Non-goals

- Localization or translations. Central wording does not imply either.
- Warnings, suppression, or any severity below an error. Hexal has one error
  class per category and no warning concept; adding suppression would be a
  language-surface decision with its own spec.
- Changing when or where a diagnostic is produced, or which phase owns it.
- An editor protocol, which deferred RFC 0242 owns.

## Implementation plan

### Phase 0: inventory and baseline

1. Record the starting revision and inventory every production path that can
   return a `compiler/types.Diagnostic`: direct literals, constructors, phase
   helpers, compilation-boundary fallbacks, and panic recovery. Use Go syntax
   trees for the authoritative constructor inventory; report any unresolved
   dynamic path before migration. Keep probes under `.tmp/` and remove them
   when the audit is recorded.
2. Group sites by stable condition and remedy. Keep a different ID for a
   different cause even if its current message matches; reuse one ID across
   parameterized instances of the same condition. Review that grouping by
   compiler stage before allocating permanent IDs.
3. Record the pre-change rendered diagnostics for the existing negative
   integration cases. A post-change comparison that removes only the added
   key must match category, wording, module, and location exactly for every
   classified source/program rejection. Record separately any case that
   currently exposes raw text from an unclassified Go error; its replacement
   is the fixed central Unknown Error, not a verbatim-text match.

### Phase 1: central package and diagnostic value

1. Add the typed ID, category/stage records, immutable query, and validation
   to `compiler/diagnostics`. Reserve retired IDs instead of reusing them.
   Add tests for duplicates, malformed identities, invalid categories or
   stages, and retired-ID handling. Keep `compiler/types.ErrorCategory` aliases
   if needed, with the central package as the one owner of category spellings.
2. Add typed message constructors there, starting with representative static,
   one-value, and multi-value messages. Their Go signatures own the contextual
   data each complete sentence needs; no phase-local prose fragments pass in.
3. Add the ID to `compiler/types.Diagnostic` and provide one constructor that
   derives category, stage, ID, and complete wording from a
   `diagnostics.Message`, plus the phase's source location. Keep `InModule`,
   span retention, ordering, and multi-diagnostic joining unchanged.
4. Make `Diagnostic.Error()` delegate the complete rendering to the central
   package. Render the descriptive key inside the existing category brackets;
   keep one output shape and exercise located and locationless errors.

### Phase 2: migrate emitters in reviewable slices

1. Migrate compile/project/profile failures, including graph-resolution
   diagnostics, logical-key and import-path validation currently carried by
   plain Go errors, the unstructured-error fallback, and panic recovery.
2. Migrate lexer diagnostics and parser helpers to take typed central
   messages, removing free-form `message string` parameters.
3. Migrate checker phase helpers and their callers by semantic facet. Do not
   turn all Type Errors or all calls to `typeErrorAt` into one ID. Move each
   complete sentence into the central package; leave only contextual values
   and the decision to report at the checker site. Replace the category-named
   helpers with one token-location adapter after their callers migrate.
4. Migrate generator diagnostics, including impossible lowering and missing
   component facts. Keep them classified as internal defects where they are
   internal defects.
5. After each slice, run its focused tests and update exact rendered-error
   assertions without weakening checks on wording or location. No slice may
   change the earliest phase that owns an error.

### Phase 3: completeness and conformance

1. Add the syntax-tree guard described above and mutation tests showing that
   an undeclared emitted ID, an active orphan ID, a direct key-less
   diagnostic construction, and a free-form message helper each fail with a
   useful location.
2. Run the negative-case baseline comparison, the ordinary suite, vet, and
   formatting checks. Confirm that generated C and the snippet manifest are
   byte-identical.
3. Update `docs/reference.md` once, after behavior stabilizes, for the new
   rendered diagnostic format. Review every `CompilationResult.Stderr`
   consumer, including the driver and workbench, for assumptions about the
   bracket prefix; retain `[]string` and diagnostic ordering.
4. Rebuild and restart `hexal play` before handoff, as the repository's
   implementation workflow requires.

## Required sweep

- Replace all production `Diagnostic{` and `NewDiagnostic(` paths in the
  compiler, including `compile.go`, `project.go`, `profile.go`, lexer, parser,
  checker, generator, panic recovery, and unstructured-error conversion.
- Change parser `errorAt`/`errorAtCurrent` and lexer `literalDiagnostic` from
  free-form `message string` to a typed central message. Replace checker
  `typeErrorAt`/`nameErrorAt`/`moduleErrorAt`/`semanticErrorAt`/`unknownAt`
  and `configurationErrorAt` with one typed token-location adapter. Replace
  compile reachability's raw-string `record`/`recordCategory` path with typed
  central messages. Remove superseded raw-string and category-selecting
  overloads rather than leaving compatibility paths.
- Remove user-visible diagnostic sentence assembly from emitting phases.
  `fmt.Errorf` remains legal for an internal Go error only when the public
  compiler boundary converts it to a centrally worded `Unknown Error`.
- Review exact-output test assertions, `CompilationResult.Stderr` consumers,
  `compiler/types.ErrorMessages`, and `docs/reference.md` after behavior
  stabilizes. Do not change source-language or generated-C behavior.

## Validation

This section is exhaustive.

- `compiler/diagnostics` is the single owner of all compiler diagnostic
  sentences, category/key bracket rendering, and source-location suffixes.
  It imports no compiler stage or `compiler/types`; multiple files may share
  this one package. No JSON generator or generic `...any` message formatter
  exists.
- Its typed constructors accept contextual values, return complete messages
  with registered descriptive keys, and cannot accept a preassembled English
  fragment as a substitute for a condition-specific argument.
- Checker diagnostics use one token-location adapter; no phase-local helper
  chooses a category or accepts free-form message text. Parser, lexer, and
  compile-reachability diagnostic helpers likewise accept typed central
  messages rather than finished English fragments.
- Every active key follows the specified grammar, is unique, maps to exactly
  one category and owning stage, and is never generated from English text.
  Retired keys remain reserved and cannot be reused for another condition.
- Every failed `compiler.Compile` returns only key-bearing, centrally worded
  diagnostics. This includes configuration and module resolution, lexer,
  parser, checker, generator, raw-Go-error fallback, and panic recovery. An
  unstructured error and a panic do not expose raw Go text or host paths.
- A renderer unit case constructed from the central unknown-variable message,
  logical module `app.hex`, line 3, and column 7 renders exactly `[Name Error
  name.unknown-variable] unknown variable score at app.hex:3:7`; a
  locationless diagnostic has no `at ...` suffix. The public
  `CompilationResult.Stderr` type remains
  `[]string`, with one rendered diagnostic per entry and unchanged ordering.
- Removing only the inserted key from each existing classified negative-case
  diagnostic restores the recorded baseline's category, sentence, module,
  line, column, and order. Existing user-correctable logical-key/import-path
  errors keep their messages after conversion from plain Go errors to typed
  central diagnostics. An unexpected unclassified Go error instead produces
  the fixed central Unknown Error and never exposes its old raw text. Identity
  migration does not move an error to a different phase or change which
  programs are accepted.
- Rewording a central message changes neither its key nor a test assertion on
  that key. Focused tests still verify wording where it is a contract.
- The two-way registry/emitter guard fails with a useful location for an
  undeclared emitted key, an active orphan key, a direct key-less diagnostic
  construction, or a free-form diagnostic-message helper. No registry record
  duplicates its message text in another table.
- `docs/reference.md` states the new rendered form, and driver/workbench
  consumers and exact-message tests use it deliberately rather than accepting
  changed strings by blanket regeneration.
- Generated C is byte-identical; the existing snippet manifest changes no
  hash. Ordinary tests invoke no external C toolchain.
- `go test ./...`, `go vet ./...`, `gofmt -l`, and `git diff --check` pass.

## Implementation readiness

Implementation-ready. The central package, typed message boundary, permanent
descriptive-key form, complete migration, output shape, guard, and exhaustive
validation are settled. This is a large mechanical rewrite, but it adds no
language syntax, warning framework, editor protocol, or second formatting
path.
