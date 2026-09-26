# RFC 0239: chibicc Review Findings

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; F1 implemented and validated 2026-09-26. The other eleven
  findings remain recorded dispositions
- Created: 2026-09-24
- Updated: 2026-09-26
- Origin: executes RFC 0163's review of chibicc and carries its twelve curated
  learnings with adoption dispositions as a durable record rather than a
  `.tmp/` spike report that is expected to be deleted. It merges two independent
  passes over the same pinned revision. They reached the same dispositions
  everywhere they overlapped and together found one actionable defect
- Reviewed revision: chibicc `90d1f7f199cc55b13c7fdb5839d1409806633fdb`, branch
  `main`, dated 2020-12-07, from `https://github.com/rui314/chibicc`. Every
  chibicc citation below is a file and line in that revision and is reproducible
  by cloning it
- Coordinates with: closed RFC 0163 (the review that requested this), deferred
  RFC 0191 (advanced C interoperability, which owns F2's surfaces), deferred
  RFC 0209 (external-package conformance, F7 at library scale), and archived
  RFC 0238 (declarative fact guards, whose guards are test-only where F1 is a
  production-code invariant); RFC 0245 consumes the completed scope invariant
  before simplifying other generator state
- Does not update `docs/reference.md`: no syntax, semantics, signature,
  diagnostic, or generated-C contract moves

## Summary

chibicc is 8,916 lines across 10 files — smaller than Hexal's generator package
alone. It buys that with choices Hexal cannot make: no diagnostic recovery, no
incremental compilation, one target, and never calling `free`. The useful
output of the review is therefore not "be smaller"; it is a short list of
techniques that transfer, and an equally short list that provably does not.

Twelve findings. **One is actionable**; the rest confirm decisions Hexal has
already made, which is itself the useful result — a review whose main output is
"the existing direction survives contact with a very different compiler" has
closed questions rather than opened them.

| # | Finding | Disposition | Owner |
| --- | --- | --- | --- |
| F1 | State the generator's scope invariant instead of defending it | **ADOPT** | this spec |
| F2 | Re-parse C declarators rather than backtrack | REJECT | Clang's typed AST remains authoritative; RFC 0191 extends normalization only on concrete demand |
| F3 | Denominate layout in bits when bitfields exist | REJECT | C compiler remains the layout authority |
| F4 | Lexical flags carried on the token | REJECT | — |
| F5 | Prosser hidesets for macro expansion | REJECT for Hexal core | — |
| F6 | Byte-offset positions, line/column computed lazily | already done, better | closed |
| F7 | Link reference-compiler objects into the test suite | already covered where useful | deferred RFC 0191 for the unsupported reverse-call direction; RFC 0209 at library scale |
| F8 | Forward-only pipeline with direct data between stages | already done | `compiler/compile.go` |
| F9 | C's usual arithmetic conversions permit implicit behaviour Hexal rejects | REJECT | retains Hexal's explicit numeric rules |
| F10 | Layout and ABI facts belong to the backend, not the frontend | REJECT duplication | C23 backend stays the authority |
| F11 | Source-line plus Unicode-aware caret presentation | REJECT for now | no correctness gap |
| F12 | Small, always-green feature slices | process observation | no code change |

F1 is the only entry that changes code. The other eleven findings are retained
because recording why a superficially attractive technique does not fit Hexal
prevents the same proposal being re-derived from this review.

## F1 — State the generator's scope invariant instead of defending it

**ADOPT.** This is the whole actionable content of the review.

### What chibicc does

`codegen.c:7` declares `static int depth`, incremented by `push` and
decremented by `pop` (`codegen.c:31-39`). After emitting each function body,
`codegen.c:1568`:

```c
gen_stmt(fn->body);
assert(depth == 0);
```

One assertion, evaluated on every compilation, catching every unbalanced
push/pop anywhere in a 1,595-line code generator. No fixture, no external tool,
no test case per construct: a new node kind that leaks a push fails on the first
program that exercises it.

### What Hexal does

Hexal has the same invariant and never states it. `expressionValidation` carries
`activeScopes []map[checker.BindingID]bool` in
`compiler/generator/render_state.go`. Scope operations are spread across
statement rendering, validation, declarations, and for-in lowering; use the
declaration and helper names below rather than pre-split source coordinates.

The invariant **already holds for production scoped walks**. The current
generator has eleven production `expressionValidation` construction sites.
Seven own a scoped validation or emission walk; each pushes the root before
allocating a binding into it:

| Site | Establishes the root scope for |
| --- | --- |
| `validateCheckedProgram` | project-statement validation |
| `validateFunctionDeclaration` | a validated function declaration |
| `validateMethodDeclaration` | a validated method declaration |
| `writeFunctionDefinition` | an emitted function declaration |
| `writeMethodDefinition` | an emitted method declaration |
| `writeLocalHelperDefinitions` | a local helper |
| `emitModulePair` | the root body |

Four other constructions are deliberately unscoped: `validateConstantOperand`
uses a temporary state for constant-object validation; `renderOperand` and
`renderExpression` use temporary literal-registry states; and
`emitModulePair` uses `moduleValueRenderState` for static module-value
initializers. The latter also constructs `renderState` for non-root modules,
but enters its scoped statement walk only for the root. These states do not
establish the root-scope invariant because they do not own a statement-scope
walk. If one later starts such a walk, its caller must establish a root
explicitly rather than rely on an automatic repair.

Every row in the table owns fresh scoped state. The root scope is never popped:
the state is discarded when its project walk, function, method, helper, or
root-body walk is done. The chibicc analogue is therefore not "depth returns
to 0" but "depth returns to 1, and the original root was never removed".

### The actual defect

Four sites defend against an empty stack that the seven scoped owners above
guarantee cannot occur on their production paths:

```go
// render_statements.go, writeStatementsAt
if len(state.activeScopes) == 0 {
	state.pushScope()
	defer state.popScope()
}
```

```go
// render_state.go, allocateBinding
if len(state.activeScopes) == 0 {
	state.pushScope()
}
state.activeScopes[len(state.activeScopes)-1][id] = true
```

with the same shape at `render_state.go` (`registerCapture`) and
`validation.go` (`validateStatements`). On every real path the stack is
already non-empty, so all four conditionals are dead.

They are not harmless. Two of them (`allocateBinding`, `registerCapture`) push
**without a matching pop**, so if the guarantee ever broke, the repair would
silently invent a scope and leave it on the stack rather than failing — turning
a structural bug into a wrong `bindingActive` answer, which feeds generated-C
correctness.

And the genuine hole is `popScope` itself in
`compiler/generator/render_state.go`:

```go
func (state *expressionValidation) popScope() {
	if len(state.activeScopes) > 0 {
		state.activeScopes = state.activeScopes[:len(state.activeScopes)-1]
	}
}
```

An over-pop — the exact bug chibicc's assertion exists to catch — is silently
absorbed. Nothing anywhere reports it.

The four repairs are dead on production paths, but focused unit tests currently
call `writeStatementsAt`, `validateStatements`, or `allocateBinding` with a
zero-value state and therefore exercise them. Those tests must establish the
same explicit root precondition as production before the repairs are removed.

So Hexal has seven production statements of the invariant, four automatic
repairs that hide invalid internal use, and one place that swallows a real
violation. What it does not have is the invariant checked at the boundary of
each fresh state owner.

### The change

1. `popScope` returns `error` and fails closed when the stack has zero or one
   scope. Depth one is the permanent root; removing it is already a generator
   contract break. Every caller propagates the existing generator
   `[Unknown Error]` with message `generator scope stack has no nested scope to
   remove`. No panic or hidden error field is introduced.
2. After each of the seven owner walks succeeds, one shared helper verifies
   that exactly the original root scope remains. The owner passes a stable
   label such as `validated method` or `root body`, which the diagnostic names.
   Checking owner boundaries is the direct analogue of chibicc's one assertion
   after each generated function; recursive block walks need no extra check.
3. The four conditional repairs are deleted. Production establishes their
   precondition explicitly, and focused unit tests are updated to do the same.
4. `popScope` and the owner-boundary helper receive direct unit tests: removing
   the root fails, and a leaked child scope fails with the owner label.

Cost: one comparison per independent validation or emission owner, ordinary
error propagation at the existing pop sites, no external tool, and no generated
code change.

### Non-goals for F1

- No change to when or where production scopes are pushed. The seven owners keep
  their current responsibility.
- No new diagnostic class. An over-pop reuses the existing generator
  contract-break category.
- No change to generated C. This is an internal consistency check.
- A failed owner walk discards its state, so it need not unwind every nested
  scope before returning the original error. A `popScope` that actually runs
  must still propagate its own invariant error; successful owner walks must
  pass the depth-one boundary check.

## F2 — Re-parse C declarators rather than backtrack

**REJECT for Hexal.**

`parse.c:681-705` handles all of C's "declaration follows use" declarator
grammar in 25 lines. On `(`, it parses the inner declarator **twice**: once with
a throwaway `Type dummy = {}` purely to find where the group ends
(`parse.c:687`), then the type suffix with the outer type, then the same tokens
again with the now-complete type (`parse.c:690`). No backtracking machinery, no
intermediate declarator structure, no fixup pass.

It is legal only because every parse function is pure:
`f(Token **rest, Token *tok)` takes the current token and writes the next back
through `rest` (`parse.c:114-131`), so the parser holds no cursor state and any
function can be called speculatively.

Hexal's `Parser` is a stateful struct with `current`, `braceDepth`,
`bodyDepth`, `blockStack`, and accumulated `diagnostics`
(`compiler/parser/parser.go:14-22`). That is the correct choice for a compiler
that recovers and collects diagnostics, which chibicc does neither of — but it
means speculative re-parsing would require saving and restoring four fields, so
the technique is unavailable by construction rather than by preference.

Hexal's own type syntax is prefix (`Ptr<mut Int32>`, `Array<T, N>`) and has no
declarator problem. Hexal's C import path goes through Clang's typed AST rather
than a hand-written declarator parser, which is the right trade and keeps the
string-in/string-out compiler boundary intact. Deferred RFC 0191 explicitly
retains that boundary for function pointers, variadics, bit-fields, unions, and
flexible arrays: future work extends typed-AST normalization or uses a wrapper;
it does not parse C declarations inside Hexal.

The compactness is also not the whole story: 25 lines parse the grammar, but
arrays, functions, qualifiers, and decay still have to be encoded somewhere.
chibicc pays that cost in `type.c`; an importer pays it in the mapping.

## F3 — Denominate layout in bits when bitfields exist

**REJECT under the current backend contract.**

The whole struct layout algorithm is 30 lines (`parse.c:2686-2712`) and works
**in bits throughout**, converting to bytes only at the end. Bitfields are not a
special case bolted onto a byte-based layout; ordinary members are members whose
width is `size * 8`. A zero-width anonymous bitfield is three lines
(`parse.c:2689-2692`) because "affects only alignment" is literally what the
code says in a bit-denominated model. Union layout is 10 lines
(`parse.c:2726-2733`) and assigns no offsets, because they are already zero.

Hexal emits C structs and lets the C compiler lay them out, so it needs none of
this. Deferred RFC 0191 also requires the C compiler to own foreign layout and
forbids Hexal from deriving offsets or masks. A future Hexal-owned packed
representation would be a new language feature with its own specification; it
must not acquire a design precondition from this review.

## F4 — Lexical flags carried on the token

**REJECT.**

`chibicc.h:88-89` puts `at_bol` and `has_space` on every token so the
preprocessor never re-inspects source text: `#` is a directive only at
beginning-of-line, and stringizing reproduces spacing. This is why chibicc needs
no separate preprocessing-token type.

Hexal has no preprocessor and should not grow these fields.

## F5 — Prosser hidesets for macro expansion

**REJECT for Hexal. Noted for C ingestion.**

Macro recursion is stopped by a hideset per token (`chibicc.h:90`) — the set of
macros a token was expanded from — unioned on object-like expansion
(`preprocess.c:647-648`) and **intersected** at the closing paren for
function-like macros (`preprocess.c:672`). The header comment
(`preprocess.c:15-23`) cites the standard's basis document.

The intersection at `)` is the subtle part and the reason naive depth-limiting
implementations mis-expand. Relevant only if Hexal ever expands C macros itself
instead of delegating to a C compiler. The current design delegates, correctly.

## F6 — Byte-offset positions with lazily computed line and column

**Already done, and Hexal's version is better. Recorded as closed so it is not
re-proposed.**

chibicc stores a raw `char *loc` into the original buffer (`chibicc.h:79`) and
computes a line number only when an error is reported, by scanning from the
start of the file (`tokenize.c:53-56`). `display_width` (`tokenize.c:44`) places
the caret correctly under East Asian wide characters. Every error then calls
`exit(1)` (`tokenize.c:61,68`): no list, no recovery, no second error.

Hexal reached the same core idea independently and went further.
`span.Span` is `{File, Start, End}` byte offsets
(`compiler/span/span.go:34-38`), and `span.Table` converts to line and column on
demand by **binary search** over precomputed line starts
(`compiler/span/span.go:100-105`) rather than a linear rescan. Hexal's spans
carry an `End`, so a diagnostic underlines a range instead of pointing at a
character, and a zero-width span is a defined insertion point
(`compiler/span/span.go:30-33`). Hexal collects diagnostics and continues.

No change.

## F7 — Link reference-compiler objects into the test suite

**Already covered where useful. No new status entry.**

chibicc's suite is 41 C programs plus a shell driver (`test/driver.sh`). Each
test is written in the language being compiled and asserts its own expectations
— `ASSERT(21, 5+20-4)` in `test/arith.c:5`, where the macro stringizes the
expression so a failure prints the source that produced it (`test/common:5-12`).

The part with no exact Hexal analogue: `test/common` is compiled by the *system*
compiler, not by chibicc, and linked against chibicc-compiled objects. One run
therefore tests compiler correctness and ABI conformance against real gcc
simultaneously.

Hexal has both halves already, further along than a first reading suggests.
`compiler/tests/c23validation` holds 140 fixtures and compiles and runs them
under Clang, and the ABI-boundary cases are named fixtures rather than an
aspiration: `foreign-scalars`, `foreign-opaque-and-record`,
`foreign-constants-globals`, `foreign-buffer-bridge`, `foreign-call-runs`, and
`foreign-record-by-value-runs`. That last one is the by-value aggregate case
`test/common` exists to cover.

Two chibicc cases have no Hexal fixture — varargs and many-argument calls — and
correctly so: neither is a Hexal surface. Deferred RFC 0191 owns them, and a
fixture for a construct the language cannot express would test nothing.

The driver also compiles a foreign source independently, links its object with a
full Hexal build, and runs the result. Object linking itself has no call
direction. The genuinely unsupported direction is a C function calling an
exported Hexal function, which requires the C-export surface already deferred
under RFC 0191. It is not a free test-only extension and does not need another
status entry. Deferred RFC 0209 remains the owner of library-scale import and
link conformance.

## F8 — Forward-only pipeline with direct data between stages

**Already done.**

chibicc runs `tokenize` → `preprocess` → `parse` → `codegen`, each stage
consuming the previous stage's output and never calling backward. Types are
computed during parsing by `add_type` in `type.c` rather than in a separate
pass; there is no symbol-table pass, no IR, and no optimizer.

Hexal's pipeline is the same shape and already documented as forward-only with
no analyzer pass, with analysis living beside the facts it consumes. The
agreement is worth recording because it is the one architectural decision both
compilers made independently.

## F9 — C's usual arithmetic conversions

**REJECT.**

chibicc implements C's usual arithmetic conversions faithfully, and they are
concise. They are also exactly the implicit behaviour Hexal removes on purpose:
silent integer promotion and silent signed/unsigned conversion at operator
boundaries.

Hexal retains its explicit numeric rules and emits C casts only after its own
checking has decided the conversion. Reading a compact, correct implementation
of the C rules is a good way to confirm that inheriting them would be a
regression against language goal 16, not a simplification.

## F10 — Layout and ABI facts belong to the backend

**REJECT duplication.**

chibicc must compute struct layout, alignment, register classification, and the
SysV calling convention itself, because it emits machine code. `push_struct` and
the two-pass `push_args2` (`codegen.c:445,456`) exist only for that reason: a
two-pass walk assigns register classes before emitting.

Hexal emits C23 and hands every one of those facts to the C compiler. This
finding strengthens that decision rather than qualifying it, and is recorded so
the "we could compute layout ourselves" question stays closed. F3 is the narrow
exception: the *unit* to use if Hexal ever computes a layout for its own
reasons.

## F11 — Source-line plus Unicode-aware caret presentation

**REJECT for now.**

Beyond F6's position model, chibicc prints the offending source line with a
caret, and `display_width` (`tokenize.c:44`) places that caret correctly under
East Asian wide characters.

This is diagnostic *presentation*, not correctness, and Hexal's diagnostics
carry the spans a renderer would need. Adding a source-line renderer is a CLI
decision that would want its own spec; there is no correctness gap and no
status entry is warranted.

Two of the three review passes raised this independently and both landed on
defer rather than adopt. If it is ever taken up, the span model it would consume
is RFC 0221's, and `unicode.c:181-189` is the reference for the width table.

## F12 — Small, always-green feature slices

**Process observation. No code change.**

chibicc's history is one always-compiling commit per language feature, with that
feature's test file added in the same commit. RFC 0163 asked for this to be
recorded separately from the architectural findings, and it is: the project
already has spec Validation sections, manifest discipline, and repository gates
serving the same end. Nothing here proposes changing how this project works.

## Supplemental proposals rejected during consolidation

These five proposals were raised while the two review reports were consolidated.
They are recorded separately from RFC 0163's twelve findings and do not add
implementation work.

### A — Add a Hexal C preprocessor using hidesets, token flags, and re-lexing

**REJECT.** A review pass proposed adopting three chibicc preprocessor
techniques — Prosser hidesets (`preprocess.c:111-148,630-683`), the `at_bol` /
`has_space` token flags (`tokenize.c:10-13,100-112`), and re-lexing `#`/`##`
output through the single lexer (`preprocess.c:221-224,499-508`) — and called
them the substantive result, owned by RFC 0039 for header parsing.

All three are good techniques and none is adoptable, for a reason that is a
fact about this tree rather than a preference. **Hexal has no C preprocessor and
no C parser to put them in.** `internal/driver/frontend.go:77` runs the selected
Clang with `-E` to preprocess a header, and `frontend.go:88` runs the same Clang
with `-Xclang -ast-dump=json -fsyntax-only` to parse the preprocessed text. The
compiler consumes Clang's JSON AST. There is no hideset to maintain because
Hexal expands no macros, no token to hang `at_bol` on because Hexal tokenizes no
C, and no synthesized text to re-lex.

Adopting them would mean *building* a C preprocessor that does not exist, which
is the largest possible violation of "never reinvent a C23 or toolchain
facility" and of language goal 3. It would also reintroduce, inside Hexal,
exactly the sub-language chibicc's own author describes as a language
implementation of its own.

Two further reasons the proposal cannot stand as written: RFC 0039 is closed and
archived, so it cannot own new work, and delegation to Clang is what keeps the
compiler core string-in/string-out — the preprocessing runs in the driver, not
in the compiler.

This is the same conclusion as F5, reached from the opposite direction, and it
is recorded separately because the proposal named an owner and a disposition
that would otherwise look settled.

### B — Revive the dormant C23 canaries

**REJECT — already done. The premise was stale documentation, not the tree.**

A review pass proposed reviving the c23 canaries as Hexal's analogue of
chibicc's stage-2 self-compilation gate, and adding a shared ABI edge-case
prelude modelled on `test/common`, both owned by RFC 0125. It cited
`AGENTS.md`, which states the canaries are *"currently dormant — their entry
points are named in lower camel case, so Go collects none of them"* and that
*"no test compiles or executes generated C."*

That premise was not true of the tree. `compiler/tests/c23validation` holds **15
runnable `Test` functions** — among them `TestC23SuiteLeak`,
`TestReleaseLaneFixtures`, `TestReleaseExecutablesAreSmallerAndUndebuggable`,
and `TestFloatingOutputIsModeIndependent` — with `buildGeneratedC`,
`runGeneratedC`, and `trapGeneratedC` helpers and **140 fixtures**. The five
lower-camel functions that remain are those helpers, not dormant canaries. The
tagged lane compiles and executes generated C today.

The ABI-prelude half is also covered where it applies: `foreign-scalars`,
`foreign-opaque-and-record`, `foreign-constants-globals`,
`foreign-buffer-bridge`, `foreign-call-runs`, and `foreign-record-by-value-runs`
are all fixtures. Varargs and many-argument calls have no fixture because they
are not a Hexal surface; deferred RFC 0191 owns them, and a test for an
unexpressible construct would be testing nothing.

The stale `AGENTS.md` rule found by this proposal was corrected during the
review: the current rule states that the tagged package is live, starts external
processes, and compiles and runs generated C. No RFC 0239 implementation work
remains for this proposal.

### C — Adopt a `calloc`-only, never-free memory policy

**REJECT.**

chibicc never calls `free`; every allocation lives to process exit
(`README.md:169-178`). It is a legitimate choice for a short-lived batch process
and it removes an entire class of bug from the compiler.

It is unavailable to Hexal twice over: the compiler is Go and garbage-collected,
so the policy is not expressible, and generated programs use the mimalloc-backed
heap with explicit cleanup, which is the language's memory model rather than an
implementation detail that could be swapped.

### D — Defer keyword recognition until after preprocessing

**REJECT.**

chibicc tokenizes keywords as identifiers and converts them in a later pass
(`tokenize.c:158-178,455-462`), because a macro name may be spelled like a
keyword and the preprocessor must run first.

Hexal has no preprocessor, so the ordering problem does not arise. Its keyword
table is read at lex time, and the contextual keywords that *are* ambiguous —
`extern`, `from`, `c`, `opaque`, `constant`, `global`, `std` — are handled by
parser context and are now machine-checked against the grammar by archived RFC
0238's guard.

### E — Cite each grammar production above its parser function

**REJECT.**

chibicc writes the EBNF production as a comment directly above the function that
implements it, one function per rule (`parse.c:361-368,654-656,668,680,707`):

```c
// declarator = pointers ("(" ident ")" | "(" declarator ")" | ident) type-suffix
static Type *declarator(Token **rest, Token *tok, Type *ty) {
```

It reads well, and in chibicc it is the *only* grammar there is — the comments
are the specification.

In Hexal they would be a second one. `GRAMMAR.ebnf` is normative,
`docs/reference.md` links it as authoritative, and `TestGrammarIsVerifiable`
parses and verifies it. A comment restating a production is a copy that nothing
checks, drifts silently, and is exactly the duplicate-authority pattern RFC 0229
and archived RFC 0238 were written to remove. It also sits badly with the CARE
comment rule, which requires a comment to carry a Contract, Architecture,
Rationale, or Edge fact the code does not already convey; a restated production
conveys what the grammar file already states, one file away.

The machine-checked version of this idea — asserting that every syntactic
production has a parser function and every parser function has a production —
was considered and is not proposed. The mapping is not one-to-one: helper
productions like `pointers` and `type_suffix` have no corresponding Hexal
function, and Hexal's parser has helpers with no production, so the guard would
need an allowlist large enough to weaken what it proves. Archived RFC 0238
already ties the grammar to the compiler where the mapping *is* exact, at the
terminal and keyword level, and that guard found the missing `unsafe`
production. A second, fuzzier tie is not worth its false positives.

## Validation

This section is exhaustive. It covers F1; the other eleven findings and five
supplemental proposals are dispositions and carry no implementation.

- `popScope` returns `error`. At depth zero or one it leaves the state unchanged
  and returns the existing generator `[Unknown Error]` with message `generator
  scope stack has no nested scope to remove`.
- Every production `popScope` caller propagates that error. No caller panics,
  discards it, or records it in hidden mutable state.
- A shared owner-boundary helper accepts the expected owner label and requires
  exactly one active scope. A mismatch returns generator `[Unknown Error]` with
  message `generator scope depth mismatch after <owner>`.
- All seven fresh-state owners call that helper after their successful project,
  function, method, helper, or root-body walk. Their labels are stable and
  distinguish validation from emission.
- Direct unit tests prove that removing the root fails without mutating it and
  that one leaked child scope fails with the supplied owner label.
- The four conditional repairs in `writeStatementsAt`, `allocateBinding`,
  `registerCapture`, and `validateStatements` are removed.
- Focused tests that directly invoke `writeStatementsAt`, `validateStatements`,
  or `allocateBinding` establish one root scope explicitly before invoking the
  helper. Production and test callers therefore share one precondition.
- Each of the seven production owners still establishes exactly one root scope
  before allocating a binding. No production scope push moves to a different
  semantic boundary.
- The four unscoped construction paths remain outside the scoped-walk
  invariant; none relies on an automatic repair to enter statement rendering
  or statement validation.
- Every existing integration case passes and no existing snippet-manifest hash
  moves. The implementation changes no generated byte.
- `go test ./...`, `go vet ./...`, `go vet -tags c23 ./...`, and `gofmt -l`
  pass; `gofmt -l` prints no path.
- No new diagnostic class, language surface, reference rule, or generated-C
  contract is introduced.

## Required sweep

- Update `TestWriteStatementsRejectsLoopControlOutsideGeneratedLoop`,
  `TestWriteStatementsRejectsNestedDeclarationsInModuleBlocks`, and
  `TestRenderTruthinessConditions` to establish their root scope explicitly.
- Update `TestValidateStatementsContinuesPastNoValueStatements` and
  `TestValidateStatementsAcceptsDeferredNoResultCallAlone` in the same way.
- Search every direct `allocateBinding`, `registerCapture`, `writeStatementsAt`,
  and `validateStatements` call in generator tests; any call constructing a
  zero-value `expressionValidation` must either establish the root or be an
  intentional new fail-closed invariant test.
- Search every `popScope` call after its signature changes and propagate its
  error. No ignored return is permitted.
- Retain no comment describing automatic root repair; the repair no longer
  exists.

## Implementation plan

### Phase 1: invariant primitives

1. Change `popScope` to return `error`, reject depth zero and depth one with the
   specified diagnostic, and leave the slice unchanged on rejection.
2. Add the owner-boundary helper that requires depth one and names its owner in
   the specified diagnostic.
3. Add direct unit tests for root-pop rejection, unchanged state after failure,
   a balanced owner, and a leaked child scope.

### Phase 2: production owners and callers

1. Propagate `popScope` errors from every render, validation, and for-in call
   site without changing the order of successful scope operations.
2. Add the owner-boundary check after each of the seven successful fresh-state
   walks: project validation, function validation, method validation, function
   emission, method emission, local-helper emission, and root-body emission.
3. Re-inventory all eleven production state constructions. Confirm the four
   auxiliary uses remain unscoped and do not need a root boundary check.
4. Delete the four automatic repairs. Run generator tests after each deletion
   so any overlooked caller is attributable to one removed repair.
5. Keep this invariant change separate from RFC 0245's total-state
   construction: the latter may remove remaining nil-map guards only after
   this RFC's scope checks and regression gates have landed.

### Phase 3: focused-test migration and gates

1. Apply the Required sweep to direct helper tests and make their root
   precondition explicit.
2. Run the generator package and full ordinary gates.
3. Verify the snippet manifest is byte-identical, then run the tagged vet gate
   and formatting check.

## Implementation readiness

Implementation-ready. Every claim above was read from the pinned chibicc
revision or from this tree, and the central one was checked in the direction
that would have invalidated it. Tracing every fresh state found seven production
owners; all establish the root explicitly before allocating bindings. Tracing
the direct unit-test callers separately found the zero-value states that rely on
the repairs today, and the Required sweep names them.

That correction is why F1 is a small, safe change rather than a restructure: the
work is to state a guarantee the code already provides and to stop swallowing
the one violation it would not survive.

The two RFC 0163 review passes agreed on every disposition they shared. The
supplemental proposals were checked against production source during
consolidation and rejected where the proposed work already existed or would
duplicate Clang. Every disposition is therefore anchored to the current tree or
the pinned chibicc revision rather than to an unchecked documentation claim.
