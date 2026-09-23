# RFC 0238: Guards for the Declarative Facts That Already Exist

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented. All five items landed and every Validation bullet was
  verified on the tree; each guard fails when its fact drifts. The driver derives
  its pack order from `specdata.Dependencies()`
- Created: 2026-09-23
- Origin: RFC 0229 gave `compiler/specdata` ownership of components,
  constructors, corelib modules, error kinds, methods, operators, scalars, and
  targets, with a cross-domain `Validate()`. Its closing audit connected each
  surviving second authority *inside Go*. Four surfaces that must agree with a
  declarative fact are not Go and were never connected: hand-written runtime C,
  the normative EBNF grammar, the checker's analysis passes, and the driver's
  pack manifest. Each is correct today and nothing keeps it correct
- Depends on: `compiler/specdata` and its records, `GRAMMAR.ebnf`, and the
  existing coverage-guard pattern — `expression_dispatch_coverage_test.go`,
  `component_registry_test.go`, `error_component_test.go`,
  `error_inventory_test.go`, `trap_inventory_test.go`
- Does not update: `docs/reference.md`. Nothing here is language-visible: no
  syntax, semantics, signature, diagnostic, or generated-C contract moves

## Summary

Add four guards and delete one duplicated list. No compiler behaviour changes,
no dispatch becomes a table, and `specdata` gains no new schema field.

RFC 0229's shape is **typed records, `Validate()`, and a coverage guard that
fails when code and records disagree**. This RFC applies only the third part,
to the places the migration could not reach because they are not Go code. The
guards are test-only, so they cannot make the compiler more complex; the one
production change removes eight lines and a hardcoded list.

## Why these four and not the rest

The registry already owns these facts. What is missing is the assertion that the
non-Go surfaces still match. Each item below was measured against the tree.

### 1. ErrorKind tag spellings in runtime C

The generator derives `ErrorKind` tags from `specdata.ErrorKinds()`, but the
hand-written runtime C spells them literally:

```
compiler/generator/packages/ and compiler/corelib/runtime/
108 occurrences of hex_tag_ErrorKind_<Name>, 26 distinct names
```

Measured against the registry: 26 registered, 26 used, zero used-but-
unregistered, zero registered-but-unused. Exactly correct, and nothing checks
it. No ordinary test compiles or scans this C — a renamed variant breaks the
runtime at `hexal build` time, or in the dormant `c23` tier, never in
`go test ./...`.

**Guard.** Scan `compiler/generator/packages/*.{c,h}` and
`compiler/corelib/runtime/*.{c,h}` for `hex_tag_ErrorKind_([A-Za-z_]+)` and
assert every captured name is a declared `specdata.ErrorKindID`. Assert the
reverse too: a registered kind no C site mentions is either a real gap or a
record nothing reads, and both are worth a failing test naming the kind.

### 2. Grammar terminals against the lexer's keyword table

`GRAMMAR.ebnf` is the normative grammar — `docs/reference.md` links it as
authoritative — and it is machine-readable EBNF. `TestGrammarIsVerifiable`
parses it and verifies every rule is reachable from `Program`. That proves the
grammar is self-consistent. Nothing compares it to the compiler, so it can be
internally perfect and describe a different language than the one that ships.

It already does:

```
lexer keywords: 36, grammar word-terminals: 42
grammar terminal, not a lexer keyword (7): constant extern from global meta opaque std
lexer keyword, not a grammar terminal (1): unsafe
```

`unsafe do ... end` compiles today and is absent from the normative grammar.
RFC 0155 landed it on 2026-09-15; the grammar was edited on 2026-09-21, six days
later, and still missed it. Seven further commits have touched
`compiler/parser` and `compiler/lexer` since that edit. This is not lag; it is
the signature of a fact nothing guards.

**Guard.** Read the `keywords` map's literal keys from `compiler/lexer/lexer.go`
through `go/ast` — not by pattern; the entries are `\t"true":  True,` and a
plausible regex over them matches nothing. Collect the grammar's terminals by
parsing `GRAMMAR.ebnf` with `golang.org/x/exp/ebnf`, which the module already
requires and `TestGrammarIsVerifiable` already uses, then diff both directions
against a declared contextual-keyword allowlist.

Three extraction rules, each forced by the grammar's own conventions rather than
chosen:

- **Collect only from productions whose name begins with an uppercase letter.**
  This grammar names syntactic productions `CamelCase` and lexical ones
  `snake_case`, and keywords appear only in the former. Without this rule the
  scan also picks up `b`, `e`, `n`, `r`, `t`, and `x` from `byte_escape`,
  `string_escape`, `exponent_part`, `raw_string_literal`, and `byte_literal`,
  plus every letter of `ascii_letter`.
- **Exclude the `"meta"` sentinel.** The grammar's preamble declares it: *"The
  meta token marks one such lexer-owned token; its exact condition remains in
  the corresponding lexical and parser rules."* It appears 18 times and is
  notation, not a terminal. `"same-line"` is excluded for the same reason.
- **Keep terminals matching `^[a-z][a-z0-9_]*$`.** Punctuation and operator
  terminals are not keywords and are not this guard's subject.

The allowlist is the second half of the value. Seven terminals are deliberately
not lexer keywords, each documented as contextual in `docs/reference.md`:
`extern`, `from`, `c`, `opaque`, `constant`, and `global` are the shown terminal
only in C-interop positions and ordinary identifiers everywhere else, and `std`
is contextual by the standard-library reference rule. Today that is English
prose. Written as a Go slice the guard consumes, it becomes a checked fact,
which is what RFC 0229 did for type constructors, methods, and error kinds.

The arithmetic the guard asserts, measured on the tree: 36 lexer keywords, minus
`unsafe` which the grammar is missing, plus the 7 contextual terminals, equals
the 42 word terminals the grammar declares. After phase 2 adds `unsafe`, the
identity is exact with no subtraction.

### 3. Statement coverage in the checker's analysis passes

The `statementNode` marker has 29 implementations, 16 in the checker and 13
mirrored in the parser. Expressions have 25 implementations and a coverage
guard, `TestExpressionDispatchersCoverEveryConcreteKind`. Statements have none.

The obvious framing — "a new statement kind is silently skipped" — is wrong for
the generator and right for the checker, and the difference decides the scope:

| Site | Type switches | `default` arms | A missing case |
| --- | --- | --- | --- |
| `generator/render.go` | 1 | 1 | `[Unknown Error] unsupported checked statement` |
| `generator/validation.go` | 1 | 1 | same |
| `generator/walk.go` | 2 | 2 | `generator walker cannot visit statement of type %T` |
| `generator/sequencing.go` | 1 | 1 | `unsupported checked statement` |
| `checker/control_flow.go` | 4 | 9 | covered |
| `checker/captures.go` | 9 | 1 | **silently not analysed** |
| `checker/starvation.go` | 5 | **0** | **silently not analysed** |

Every generator switch fails closed, exactly as the architecture rules require,
so a miss there is loud. The checker's analysis passes are the opposite: a
statement form the starvation scanner does not recognise is not an error, it is
a `while true` body that never gets the must-yield rule applied. That is a
soundness hole — a program that should be rejected compiles — and it produces no
diagnostic at all.

**Guard, scoped to where a miss is silent.** Enumerate the checker's concrete
`statementNode` implementations from the AST, and assert each is reachable in
`captures.go` and `starvation.go`. The generator is excluded deliberately: its
fail-closed defaults already are the guard, and asserting over them would add a
test that can only duplicate an existing diagnostic.

Current state is correct — `UnsafeStatement`, the newest form, is threaded
through both passes. The guard keeps the next one honest.

### 4. Generated include literals against `Component.Files`

74 `"hexal/<name>.{h,c}"` string literals sit across the generator's component
builders, concentrated in `equality_component.go` (18), `module_collections.go`
(5), and `concurrency_component.go` (5). They resolve to 43 distinct filenames.
`ComponentSpec.Files` declares 43. Measured: every literal is declared, and the
two sets match exactly — the same clean correspondence as the ErrorKind tags,
and equally unenforced.

**Guard.** Assert every such literal names a file some registered `Component`
declares. This is a spelling check, not a refactor: the builders keep their
explicit includes, which is where a reader expects to find them.

The reverse direction is deliberately not asserted. A declared file that no
generator literal spells is not necessarily wrong — a component may be emitted
without any other component including it — so requiring it would encode a
coupling this RFC has not established.

### 5. The driver's pack-manifest list reads the registry

`internal/driver/runpack.go:283`:

```go
want := []string{"libuv", "mimalloc", "utf8proc"}
```

This is the acknowledged last duplicate. `component_registry_test.go:69-71`
already names it: *"The build driver keeps its own pack-manifest ordering of the
same names until a later slice removes that last consumer."* This RFC is that
slice.

**Change.** Build `want` from `specdata.Dependencies()`. The driver already
imports `compiler/types` and the central config, so the layering is established.
The ordering requirement stays: the registry's declaration order becomes the
required manifest order, which is what the hardcoded list encoded.

This is the only production change in this RFC, and it deletes a list rather
than adding a mechanism.

## Rejected

Considered against RFC 0229's decisions and declined. Recorded so they are not
re-proposed.

- **A purity or effect fact on `MethodSpec`,** to replace the ~20 hand-listed
  pure accessors in `sequencing.go`. This inverts the property that makes the
  rest of this RFC safe. Everywhere else a registry disagreement is fail-closed;
  here a wrong record is a silent evaluation-order change in generated C. Adding
  a field whose mistakes are undiagnosable is a worse trade than a hand-list
  whose mistakes are visible in one file. It also needs a language-level
  decision about what "pure" means for a method, which is a spec of its own.
- **Merging `operatorFromToken` and `operatorIdentity`, and a result-shape fact
  on `OperatorSpec`.** These add schema to delete a few switch arms. RFC 0229
  deliberately kept semantic behaviour in explicit switches and moved only
  facts; a result shape consumed by three switches is close enough to behaviour
  that the boundary stops being obvious, and the registry grows to save little.
- **The hand-written header sets in the generator's emission path.** RFC 0229's
  closing status records that facts no consumer can read without moving wording
  or behaviour were resolved out of migration scope, with their reasons recorded
  at the owning code. Reopening them re-litigates a closed ADR rather than
  finishing it.
- **An `ErrorKind` arity and header-completeness guard.** Already implemented:
  `error_component_test.go:101-107` asserts `len(ErrorKindVariantNames)` equals
  the registry count and that every entry matches by index.
  `compiler/types/error_kind.go` derives the array from `specdata.ErrorKinds()`
  and its comment states the arity is the exported API's contract rather than a
  second declaration of the variant set.
- **Turning any dispatch switch into a data-driven table.** The architecture
  rules require explicit dispatch over every supported node, and RFC 0229 states
  that data selects and describes supported cases while behaviour stays in
  switches. The missing piece was always the guard.
- **Re-hardcoded capacity constants in `error_inventory_test.go`.** Real, and
  too small to spend a spec on.

## Constraints

- **Test-only, except item 5.** Items 1 to 4 add `_test.go` files and change no
  compiler source, so they cannot add complexity to the compiler or reach the
  binary. Item 5 deletes a list.
- **No new dependency.** Every guard uses `go/ast`, `go/parser`, `regexp`, and
  `os` from the standard library. The third-party confinement walk inspects only
  non-test sources and is unaffected.
- **No new schema.** `specdata` gains no field and no record type.
- **Generated C is byte-identical.** Item 5 changes how a list is built, not
  what it contains; items 1 to 4 change nothing.
- **Guards name the fact, not the mechanism.** A failure says which ErrorKind,
  which keyword, which statement type, which include — never an index or an
  internal identifier.

## Implementation plan

Each item is independently landable and leaves the tree green. Order is by
value, so stopping early still banks the best of it.

### Phase 1 — ErrorKind tags in runtime C

1. Add the scan-and-compare guard beside the existing inventory tests.
2. Confirm it passes on the tree at 26 names and 108 sites.
3. Confirm it fails when a registered kind is renamed without updating the C,
   by making that edit temporarily and observing the failure name the kind.

### Phase 2 — grammar terminals against lexer keywords

1. Declare the contextual-keyword allowlist — `c`, `constant`, `extern`, `from`,
   `global`, `opaque`, `std` — with a comment stating why each is a grammar
   terminal and not a lexer keyword, and no spec number.
2. Add the guard: parse `GRAMMAR.ebnf` with `golang.org/x/exp/ebnf`, collect
   terminals from uppercase-named productions only, drop `meta` and
   `same-line`, keep word-shaped terminals, and diff against the keyword map
   read through `go/ast`.
3. Add `unsafe` to `GRAMMAR.ebnf` — the drift this guard exists to have caught.
   The rule is read off `compiler/parser`, not invented.
   `(*Parser).unsafeStatement` consumes the keyword, requires `do`, parses the
   shared block production, and requires `end`; `parser.go:490` dispatches it
   from the same switch and in the same position as `if`, `while`, and `for`,
   and `UnsafeStatement` implements both `statementNode()` and
   `topLevelItemNode()` exactly as `WhileStatement` does. So it is a
   block-carrying statement, and it belongs beside the other three rather than
   in `NonControlStatement`, whose members all carry no block:

   ```ebnf
   Statement = NonControlStatement | ReturnStatement
               | IfStatement | WhileStatement | ForStatement | UnsafeStatement .

   UnsafeStatement = "unsafe"  "do"  Block  "end" .
   ```

   The spacing matches the file's existing convention, which
   `WhileStatement = "while"  Expression  "do"  Block  "end" .` sets.
4. Confirm the guard fails before that grammar fix and passes after it, and that
   `TestGrammarIsVerifiable` still parses and verifies the grammar with the new
   production reachable from `Program`.

### Phase 3 — statement coverage in the checker's analysis passes

1. Enumerate concrete checker `statementNode` implementations from the AST,
   mirroring the expression guard's technique.
2. Assert each is reachable in `captures.go` and `starvation.go`.
3. Confirm the guard fails when a case is removed from either pass.

### Phase 4 — include literals against the component registry

1. Collect `"hexal/<name>.h"` literals from the generator's non-test sources.
2. Assert each names a file a registered `Component` declares.

### Phase 5 — the driver reads the dependency registry

1. Replace the hardcoded `want` with `specdata.Dependencies()` ordering.
2. Delete the stale sentence in `component_registry_test.go`'s comment naming
   the driver as the last consumer.
3. Confirm `hexal doctor` still passes and the pack manifests still validate.

## Validation

This section is exhaustive.

- Every `hex_tag_ErrorKind_<Name>` in `compiler/generator/packages` and
  `compiler/corelib/runtime` names a declared `specdata.ErrorKindID`, and every
  declared kind appears in at least one of those files. The guard reports 26
  names across 108 sites on the tree as it stands.
- Renaming a registered ErrorKind without updating the runtime C fails that
  guard, naming the kind.
- Every lexer keyword is a terminal in `GRAMMAR.ebnf`, and every grammar word
  terminal is either a lexer keyword or one of the seven declared contextual
  keywords. After phase 2 the two sets differ by exactly that allowlist: 43
  grammar word terminals against 36 keywords.
- The terminal scan yields no single-letter escape-sequence character and no
  `ascii_letter` alternative: restricting collection to uppercase-named
  productions is asserted, not assumed, by checking that `b`, `e`, `n`, `r`,
  `t`, and `x` are absent from the collected set.
- The `meta` sentinel is never treated as a terminal.
- `unsafe` is present in `GRAMMAR.ebnf`, and removing it fails the guard.
- `TestGrammarIsVerifiable` still passes: the grammar parses and every rule is
  reachable from `Program` after the `unsafe` addition.
- Every concrete checker `statementNode` implementation is reachable in
  `captures.go` and `starvation.go`; removing a case from either fails the
  guard, naming the statement type.
- Every `"hexal/<name>.{h,c}"` literal in the generator's non-test sources names
  a file a registered `Component` declares: 43 distinct literals against 43
  declared files, with none undeclared.
- The driver's required pack-dependency list is derived from
  `specdata.Dependencies()`, no Go source outside `specdata` spells the three
  dependency names, and `hexal doctor` passes for the shipped profile.
- `go test ./...`, `go vet ./...`, `gofmt -l`, and `go test -race ./...` pass.
- `TestNoThirdPartyImportsOutsideBenchmarks` passes and `go.mod` is unchanged.
- The snippet manifest is unmoved: no generated byte changes.
- Every guard's failure message names the fact — the ErrorKind, the keyword, the
  statement type, the include — and no guard prints an index or an internal
  identifier.

## Implementation readiness

Implementation-ready, with no open input. Every figure below was produced by
running the guard's own technique against the tree while this RFC was drafted,
so each is reproducible rather than estimated:

| Guard | Measured on the tree |
| --- | --- |
| 1 — ErrorKind tags | 26 registered, 26 used across 108 C sites, 0 unregistered, 0 unused |
| 2 — grammar terminals | 42 word terminals against 36 lexer keywords; `unsafe` missing from the grammar; 7 contextual terminals |
| 3 — statement coverage | 29 `statementNode` implementations, 16 in the checker; `starvation.go` 5 type switches / 0 defaults, `captures.go` 9 / 1 |
| 4 — include literals | 74 literals, 43 distinct, 43 declared by components, 0 undeclared |
| 5 — driver dependency list | 3 names hardcoded at `runpack.go:283`, already flagged as the last consumer |

Each guard's extraction technique is pinned rather than left to the
implementer, because three of the four have a wrong-looking obvious approach
that silently produces nothing or the wrong set:

- The lexer keyword map must be read through `go/ast`. A regex over
  `\t"true":  True,` returns zero entries, which reads as "no keywords" rather
  than as a failure.
- The grammar scan must parse the EBNF and restrict to uppercase-named
  productions. A regex over the file returns escape-sequence characters and
  every letter of the alphabet alongside the keywords.
- The CPU-cheap direction for guards 1 and 4 is a text scan of non-Go files,
  which is the established pattern in `trap_inventory_test.go` and
  `error_inventory_test.go` rather than a new mechanism.

The `unsafe` grammar rule that phase 2 adds is settled above from
`(*Parser).unsafeStatement` and the dispatcher's placement, so no design
decision remains inside any phase.
