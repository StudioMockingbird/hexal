# RFC 0103: Language Surface Audit

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; findings catalog. Not implementation-ready as a whole — each
  finding is disposed of by promotion to its own RFC, explicit rejection, or
  recorded acceptance. Dispositions are recorded in the Disposition section.
- Created: 2026-08-21
- Scope: syntax, semantics, and builtin API surface of the language as
  specified by `docs/reference.md`; evaluated against the project goals in
  `AGENTS.md`. Compiler implementation quality is out of scope.
- Coordinates with: `docs/reference.md`, `docs/status.md`, RFC 0039 (C interop),
  RFC 0094 (anonymous function literals), RFC 0027 (Arena/Pool)
- Companion: none. This RFC originates the findings; successor RFCs carry
  individual designs.

## Summary

Thirty-four findings from a full read of the normative reference against the
stated goals: cohesive and intuitive surface, one obvious way to do things,
small clean surface, everything C can do, low ceremony, no undefined behavior,
the compiler catches every memory error a local analysis can decide.

The audit's overall verdict: the foundation mechanisms — ownership-decides-
representation, lossless-widening-only conversion, traps-not-UB arithmetic,
Error-in-unions with try/errdefer, structural union interning — are coherent
and worth protecting unchanged. Nearly every finding below is an omission or an
inconsistency between two otherwise-good rules, not a wrong mechanism.

Findings are numbered F1–F34 in one flat sequence, grouped by class. Each
finding states its evidence as quoted reference text, the goal it conflicts
with, a proposed direction, and the open question its successor RFC must
answer. Nothing here changes the language until a successor RFC closes.

## Severity ordering

| # | Finding | Class | Goal conflict | Size |
|---|---|---|---|---|
| **F1** | **Mutual recursion is impossible** | expressiveness hole | everything C can do | large |
| **F2** | **`match` value mode accepts only Bool** | expressiveness hole | one obvious way | medium |
| **F3** | **No conditional expression** | expressiveness hole | low ceremony | small |
| **F4** | **Dict keys are exactly Int32 or Strand** | API hole | high-level systems | small |
| **F5** | **Strand/String comparison allocates** | API hole | no runtime overhead | small |
| **F6** | **Iterator invalidation is silent UB** | soundness | no undefined behavior | small |
| **F7** | **`try` banned at root scope** | ceremony trap | low ceremony | small |
| F8 | Universal truthiness | inconsistency | static typing rigor | small |
| F9 | `Task.yield()` contradicts "no static methods" | naming/model | coherence | small |
| F10 | `EoS` standalone type serves one sentinel | surface weight | small clean surface | small |
| F11 | `is Nil` invalid while `is T` valid on same unions | inconsistency | intuitive | small |
| F12 | Rune arithmetic banned outright | inconsistency | trap philosophy | small |
| F13 | Three divergent escape tables; `\xHH` absent from strings | lexical asymmetry | intuitive | small |
| F14 | Block comments do not nest | lexical gap | intuitive | trivial |
| F15 | Atomic rule stack complexity; 8/16-bit excluded arbitrarily | complexity | simple surface | medium |
| F16 | List has no non-trapping accessor; Dict has `find` | API asymmetry | uniform | trivial |
| F17 | Stored-Heap vs per-call-Heap dual convention | API asymmetry | one obvious way | medium |
| F18 | Narrowing only on direct locals | surprise | intuitive | medium |
| F19 | View-return safety stops at depth one | safety hole | catch memory errors | medium |
| F20 | Literal `while true` yield rule misses `while flag` | half-measure | trivial concurrency | small |
| F21 | `<...>` disambiguation heuristic corners | parse risk | if it compiles, it runs | small |
| F22 | Match arms split on unparenthesized `\|` | footgun | intuitive | small |
| F23 | Call-argument evaluation order unspecified | underspecification | determinism posture | small |
| F24 | Print quoting depth-dependent; no separator or newline form | API friction | low ceremony | small |
| F25 | Errors cannot compose; propagation erases context | capability gap | error model coherence | medium |
| F26 | `Fun<>` placement restriction table length | complexity | simple surface | small |
| F27 | `size_of` does not unlock computed Array lengths | capability gap | systems use | medium |
| F28 | One method per `impl` declaration | ceremony | low ceremony | trivial |
| F29 | No pattern alternation in match arms | ceremony | composability | small |
| F30 | No labeled break/continue | ceremony | low ceremony | small |
| F31 | ADT variants always fully qualified | ceremony | low ceremony | small |
| F32 | C interop remains draft | promise gap | goals 8–9 | tracked |
| F33 | Residual no-UB leaks beyond F6 | soundness | no undefined behavior | small |
| F34 | Local-analysis memory checks have silent holes | safety honesty | goal 18 wording | docs |

## What clicks (protected unchanged)

Recorded so successor RFCs do not regress them:

1. Ownership decides representation: owners are pointer-sized handles, inline
   or borrowing types are values. One rule explains String-vs-View and predicts
   every future type.
2. `:=` states the type on exactly one side; contextual initializers are
   rejected on inference.
3. Every body opens with an explicit delimiter (`do`/`then`/`else`) and closes
   with `end`.
4. `ref` is the only address-taking form; writability comes from the place;
   outermost-only MutPtr-to-Ptr weakening.
5. `T | Nil` uses the null niche with mandatory narrowing.
6. Implicit conversion is lossless widening only; `value.to<T>()` is the one
   explicit conversion; Size honestly has no widening edges.
7. Arithmetic edges trap before any invalid C operation executes.
8. Errors are union members; `try` normalizes; `errdefer` shares reverse defer
   order on Error exit.
9. Structural unions flatten, dedupe, canonically order, and intern.
10. `--` line comments are coherent precisely because `--` decrement is
    excluded.

## Findings

### Broken

#### F1 — Mutual recursion is impossible

Evidence: "A function may call itself or an earlier function; forward calls
and mutual recursion are unavailable." Module dependency cycles are Module
Errors, so no cross-module relief exists. `Fun<>` values cannot rescue this:
Fun is invalid as an object member, collection element, or result.

Problem: expression/term parser pairs, graph algorithms, and state machines —
core systems-programming shapes — cannot be written directly. This is the
single largest conflict with "users should be able to do everything with Hexal
that they can do with C."

Direction: file-scope prototypes, or source-order-independent visibility for
module-level functions. Order-independence is preferred: it deletes a rule
rather than adding a syntax, and matches how types already resolve.

Open question: does order-independent function visibility break any
source-order-dependent semantics elsewhere (specialization discovery, literal
registry order)?

#### F2 — `match` value mode accepts only Bool

Evidence: "`match` is an expression … Value mode matches `true`/`false`."

Problem: there is no integer switch and no string dispatch. Opcode, state-
code, and menu dispatch degrade to if/elseif ladders. A language whose
signature construct is `match` cannot match on its most common dispatch keys.

Direction: extend value mode to every equality-capable scalar (fixed-width
integers, Size, Byte, Rune, String, Strand). Patterns become literal or
constant expressions; exhaustiveness applies only where a closed domain exists
(Bool today; ADTs and unions stay in type mode).

Open question: exhaustiveness over integer ranges is undecidable in general —
value-mode integer match requires a catch-all arm; confirm this reads well.

#### F3 — No conditional expression

Evidence: `if` is a statement in the grammar; the conditional operator is
excluded; the only conditional value form is
`match c is | true then x | else then y end`.

Problem: every mainstream language expresses conditional values in 3–10
characters; Hexal needs ~40. Felt in ordinary code daily.

Direction: if-expressions (`if c then x else y end` as a primary-expression),
reusing the exact statement delimiters so there is one conditional syntax in
both positions. Alternatively accept F2's extended match as the answer and
reject this finding explicitly — but then say so.

Open question: interaction with `match` scrutinee parsing (`is` markers) when
an if-expression appears inside a match arm.

#### F4 — Dict keys are exactly Int32 or Strand

Evidence: "K is exactly Int32 or Strand."

Problem: UInt64 IDs, handles, hashes, and packed key structs are bread-and-
butter systems keys. Excluding every other fixed-width integer looks arbitrary
rather than principled — internal hashing costs nothing per additional width.

Direction: admit all fixed-width integers plus Size and Byte at minimum.
Composite/user-hashed keys stay excluded with the existing no-user-hash-
protocol stance.

Open question: generated hash specializations per key width — naming and
component placement under the RFC 0095/0100 component regime.

#### F5 — Strand/String comparison allocates

Evidence: "String and Strand are not mutually comparable"; `Strand.to_string`
requires a Heap.

Problem: comparing runtime input (Strand) against a literal forces
`label.to_string(heap) == "hello"` — a heap allocation to answer a yes/no
question. Violates no-runtime-overhead at the most common comparison site.

Direction: define String/Strand comparison over logical UTF-8 bytes (unsigned-
byte lexicographic, matching existing String ordering). Equality and ordering
both.

Open question: does cross-type comparison weaken the representation-follows-
ownership story, or is comparison naturally representation-free?

#### F6 — Iterator invalidation is silent UB

Evidence: "Structural List changes and every Dict mutation invalidate
traversal; this is programmer responsibility."

Problem: what happens is unspecified. This is undefined behavior under another
name and directly contradicts "No undefined behavior." It is the one place the
language's central promise is currently false.

Direction: generation-counter trap on structural mutation during traversal.
The container already owns its header; a bumped generation checked per step is
cheap and converts silent corruption into the standard `[Runtime Error]` path.

Open question: cost on tight loops; whether Array in-place element writes
(allowed today) remain exempt.

#### F7 — `try` banned at root scope

Evidence: "`try` and `errdefer` are valid only inside a function whose declared
result accepts Error; both are invalid at root scope."

Problem: root statements are where scripts live. Any fallible call at top
level forces hand-rolled match arms — maximum ceremony exactly where beginners
start, against low-ceremony and against the error model's own ergonomics.

Direction: permit `try` at root with root-level propagation meaning process
exit printing the Error in the direct-print format. `errdefer` stays invalid
(there is no function exit to hook).

Open question: exit status convention; interaction with scheduler shutdown
ordering when concurrency is linked.

### Out of place

#### F8 — Universal truthiness

Evidence: "Only `false` and `nil` are falsey. Truthiness applies to
conditions…"

Problem: `if 0 then`, `while "" do`, and `if someObject then` compile. On
`V | Nil` truthiness is excellent (`if d.find(k) then`); on integers and
aggregates it dissolves the Bool discipline enforced everywhere else.

Direction: conditions require Bool or a union containing Nil. The ergonomic
find-and-test idiom survives; `if 0` becomes the type error it should be.

Open question: migration count of existing programs relying on non-Bool
conditions (workbench snippets first).

#### F9 — `Task.yield()` contradicts "no static methods"

Evidence: "There is no overloading, default/named/variadic argument syntax,
static method…" versus the `Task.yield() -> no value` signature.

Problem: builtins use namespace-call syntax users cannot replicate; user types
have no way to spell the same shape. The model is coherent internally but the
reference denies the very form it uses.

Direction: either name the concept (companion functions on nominal types,
available to users via `impl` without receiver) or document Task.yield as a
protected builtin operation outside the method model. Renaming to a bare
protected operation (`yield()`) is also viable.

Open question: if companion functions become user-available, constructor
patterns (`Point.new`) arrive with them — decide whether that is wanted.

#### F10 — `EoS` standalone type serves one sentinel

Evidence: EoS is a valid standalone core type; its sole specified consumer is
`Channel<T>.receive() -> T | EoS`.

Problem: a whole scalar with no arithmetic, no dedicated print format, and one
use weighs on the "small clean surface" goal.

Direction: keep — the zero-state niche representation and the completion
signal justify it — but record the acceptance explicitly so the cost is
acknowledged. Alternative (receive returning `T | Nil` with close-drained
semantics) conflates absence with completion and is worse.

Open question: none; disposition only.

#### F11 — `is Nil` invalid while `is T` valid on the same unions

Evidence: "`is Nil` is invalid, and `T | Nil` also rejects `is T`; use
`== nil`/`!= nil`. Larger nullable unions may test non-Nil members, and match
type patterns may name Nil."

Problem: three adjacent rules point in different directions. `u is Int32` works
on `Int32 | String`; `p is Nil` fails on `Ptr<T> | Nil`; match patterns may
name Nil anyway.

Direction: allow `is Nil` uniformly (it narrows exactly like `== nil`), and
reconsider rejecting `is T` on two-member nullables — the special case exists
for the niche representation, but the surface cost is permanent.

Open question: does `is Nil` narrowing compose with the flow-fact machinery
that `== nil` already feeds?

#### F12 — Rune arithmetic banned outright

Evidence: "Rune is invalid for `+`, `-`, `*`, `/`, `%`, unary `-`, `~`, `&`,
`^`, `|`, `<<`, and `>>`."

Problem: `'a' + 1` requires convert-out/add/checked-convert-back. Elsewhere
dynamic invalidity traps; here the operation simply does not exist —
inconsistent with the trap philosophy and hostile to text processing.

Direction: define Rune addition/subtraction with Unicode-scalar validity
traps (surrogate and overflow checks), matching the conversion contract.
Multiplication and bitwise forms stay excluded.

Open question: wrapping behavior at U+10FFFF; whether `+` takes Rune or
integer addends (integer addend recommended).

#### F13 — Three divergent escape tables; `\xHH` absent from strings

Evidence: string escapes support `\' \" \\ n r t 0 u{...}`; byte-literal
escapes support `\\ \' n r t 0 xHH`; rune escapes add `\" u{...}`.

Problem: three tables differing in small ways; arbitrary bytes are unreachable
inside `"…"` (by UTF-8 validation intent, but the asymmetry with `b'\xHH'`
reads as an oversight).

Direction: unify on one escape set across all three literal kinds — the union
of current sets minus forms that cannot apply (`\u{}` in bytes stays excluded;
`\xHH` becomes valid in strings as a direct byte spelling subject to UTF-8
validation).

Open question: does `\xHH` in strings break UTF-8 validation simplicity for
multi-byte sequences?

#### F14 — Block comments do not nest

Evidence: "comment-character = ? any character not beginning the sequence
\"]--\" ?"

Problem: `--[ --[ ]--` ends at the first `]--`. Commenting out code containing
block comments fails silently-ish (early resume, cascade of syntax errors).

Direction: nest by depth counting. Trivial lexer change, standard expectation.

Open question: none.

#### F15 — Atomic rule-stack complexity; 8/16-bit excluded arbitrarily

Evidence: Atomic is non-copyable, placement-restricted to Binding and
ObjectMember, invalid as pointee, invalid under `ref`, containment traversal
stops at pointers/handles; T is "Bool, Int32, UInt32, Int64, UInt64, or Size";
"lock-freedom is not guaranteed."

Problem: each rule is individually motivated; the composite is the hardest
corner of the language to hold in your head. The width exclusion has no stated
principle — C23 `_Atomic uint8_t` exists, and lock-freedom is not promised
anyway.

Direction: admit all fixed-width integers and Bool. Keep the placement rules
but consolidate their rationale into one paragraph a reader can hold.

Open question: generated lowering for sub-word atomics on x86-64 (register-
width read-modify-write with masked stores) — confirm before promising.

#### F16 — List has no non-trapping accessor; Dict has `find`

Evidence: Dict exposes `get` (trapping) and `find -> V | Nil`; List exposes
only trapping `[]` and `pop`.

Problem: sibling collection types answer "maybe missing" differently. Length-
prechecking is the workaround and it is two statements where one reads better.

Direction: `List<T>.first() -> T | Nil` (or `try_get`), symmetric with find.
Pop keeps trapping semantics.

Open question: naming family — `first`/`last` pair or single `try_at(index)`.

#### F17 — Stored-Heap vs per-call-Heap dual convention

Evidence: List/Dict/Channel/Mutex take Heap at construction and store it;
String operations take it per call (`concat(h, …)`, `to_string(h)`,
`from_bytes(h, …)`).

Problem: two conventions for one concern. Defensible individually (container
owns future growth; String ops are one-shot), but users must remember which
family is which.

Direction: pick one. Per-call everywhere is more Zig-like and keeps types
smaller; stored-everywhere makes call sites shorter. Either is fine;
ambiguity is not.

Open question: does stored-Heap on String bloat the handle past pointer size,
breaking the ownership-rule representation promise?

### Unintuitive

#### F18 — Narrowing only on direct locals

Evidence: "Narrowing applies to direct local reads; assignment or writable
address escape invalidates it."

Problem: `if opt.value is Int32 then …opt.value…` does not narrow — member
chains do not qualify. Users must copy to a local first, and no diagnostic
explains why the fact did not stick.

Direction: keep locals-only narrowing (sound and simple) but make the checker
emit a targeted diagnostic: "narrowing applies to bindings; bind
`opt.value` to a local first." Documentation alone may suffice.

Open question: none.

#### F19 — View-return safety stops at depth one

Evidence: "A directly returned local-rooted View is rejected. Direct View
return analysis does not inspect Views nested in returned objects, ADTs,
unions, or collections."

Problem: the same dangling-View bug compiles or fails depending on whether it
was wrapped in an object. A local analysis CAN decide the nested case by
walking the returned aggregate — this violates "catch every memory error that
a local analysis can decide without … disproportionate checker complexity."

Direction: extend the walk through returned aggregates' View-typed members
recursively. Bounded by type shape, not alias analysis; proportionate.

Open question: false-positive pressure from Views legitimately rooted in
parameters nested inside returned objects — the walk must track provenance
kinds, not just presence.

#### F20 — Literal `while true` yield rule misses `while flag`

Evidence: "Every repeating path through task-reachable literal `while true`
visibly executes `Task.yield()` or compilation fails."

Problem: `while running` with a never-false flag starves the scheduler and
compiles clean. The rule catches the syntactic special case and misses the
semantic class.

Direction: either generalize to a documented starvation contract (cooperative
scheduling means any long loop must yield; document, don't check) or drop the
literal-only check as theater. Checking arbitrary loop conditions is not
decidable; the honest options are documentation or wider-but-still-syntactic
coverage.

Open question: does the workbench rely on the literal-true check educationally?

#### F21 — `<...>` disambiguation heuristic corners

Evidence: "A balanced `<...>` is generic syntax only when immediately followed
by call arguments, a qualified constructor/member, or object literal.
Otherwise `<`, `>`, and `>>` are operators."

Problem: `f(a < b, c > d)`-class parses await in argument lists. Standard-ish
(Rust infers similarly) but the failure mode is a confusing type error far
from the cause.

Direction: keep the heuristic; add a diagnostic hint when a balanced `<...>`
was parsed as comparisons and the inner tokens would typecheck as a generic
argument list. Cheap lookahead, big confusion savings.

Open question: none.

#### F22 — Match arms split on unparenthesized `|`

Evidence: "Unparenthesized `|` starts another arm"; arm expressions end at the
next `|`.

Problem: `| A then x | y then z` parses as two arms when `x | y` was meant —
and bare `y` is a valid type pattern, so the mis-parse can survive checking.

Direction: require that an arm expression containing a top-level `|` be
parenthesized (already implied) AND reject a following arm whose pattern is a
plain identifier that names a value binding in scope — the collision case.
Alternatively ban bitwise-or in arm expressions outright (parenthesize
mandatorily) with a clear diagnostic.

Open question: none.

#### F23 — Call-argument evaluation order unspecified

Evidence: "operand order, call-argument order, receiver-versus-argument order,
and object-initializer order are C23-unspecified" — while print and spawn
arguments ARE specified left-to-right.

Problem: side-effect ordering between arguments is unspecified, contradicting
the determinism posture everywhere else. The spec's own exceptions prove
left-to-right was considered and chosen where convenient.

Direction: specify left-to-right evaluation for call arguments, index operands,
and object-initializer members. Generated C already emits sequenced
statements for multi-step lowerings; this formalizes what the generator
mostly does.

Open question: does full sequencing cost optimization opportunities the
generator cares about? (Unlikely — CSE still applies.)

#### F24 — Print quoting depth-dependent; no separator or newline form

Evidence: "Direct text/Rune is raw; nested text/Rune is quoted/escaped";
"`print(arg, ...)` … inserts no separator/newline."

Problem: output shape depends on nesting depth (learnable but surprising);
every call site spells separators and newlines manually; there is no println.

Direction: keep depth-quoting (it is deterministic and documented) but add a
second protected form `println(...)` appending one newline. Separators stay
manual.

Open question: does println earn its surface weight? (Yes: it is the most
common print shape in teaching code.)

#### F25 — Errors cannot compose; propagation erases context

Evidence: Error fields are fixed and immutable; "Propagation preserves the
location"; the only constructor injects the creation site.

Problem: adding context means constructing a new Error and discarding the old
— the propagation trail is unrecoverable. For a language whose entire error
story is Error-in-unions, errors that cannot wrap or chain is a capability
gap in the model's centerpiece.

Direction: an explicit wrapping constructor `Error.wrap(header, message,
cause: Error)` with a defined nested print form, OR record acceptance that
errors are flat and context is the caller's job. Flat errors are coherent;
undecided is not.

Open question: does wrapping interact with `errdefer` cleanup ordering and
with the direct-print diagnostic format?

#### F26 — `Fun<>` placement restriction table length

Evidence: Fun is valid in bindings, parameters, parameter-inside-Fun, and
union members; invalid as result, member, collection element/value, Task
argument/result, Channel element, pointee, or ref target.

Problem: necessary given no closures, but the list is long enough to be a
memory test; the inverse statement ("Fun exists only where its storage
outlives no scope") is shorter and generative.

Direction: restate the rule positively in the reference (one sentence plus
the derived exclusions), no semantic change.

Open question: none.

#### F27 — `size_of` does not unlock computed Array lengths

Evidence: "N is a positive integer literal"; "These operations do not make
arbitrary Array lengths valid."

Problem: no const-eval exists in type position at all. `Array<Byte,
size_of<Point>()>` and offset-table patterns common in systems code are
unreachable.

Direction: constant-expression subset in Array-length position: integer
literals, `size_of`/`align_of` of complete types, and the integer operators
over those. No user constants yet (no globals exist to be constant).

Open question: does admitting `size_of` create circularity with incomplete
types? (Reject incomplete operands; already the rule.)

### Ceremony

#### F28 — One method per `impl` declaration

Evidence: `implementation-declaration = "impl" , type-expression , "." ,
identifier , … , "do" , block , "end"`.

Problem: multi-method types repeat `impl Receiver.` per method.

Direction: optional grouped form `impl Receiver do … end` containing
method declarations. Purely additive sugar; the per-method form stays valid.

Open question: none.

#### F29 — No pattern alternation in match arms

Evidence: `match-arm = "|" , match-pattern , "then" , match-arm-expression`;
the arm separator consumed `|`.

Problem: grouping variants (`Circle | Sphere => round()`) needs repeated arms.

Direction: allow alternation within one pattern using a different token —
`or` is available and unambiguous in pattern position:
`| Circle or Sphere then …`.

Open question: interaction with F2 literal patterns once value mode widens.

#### F30 — No labeled break/continue

Evidence: "`break` and `continue` target the nearest loop."

Problem: nested-loop early exit needs flag variables.

Direction: optional label on `do` openers (`while … do:outer` or a prefixed
form) with `break outer`. Low priority relative to F1–F7.

Open question: syntax placement that does not disturb the delimiter uniformity
rule.

#### F31 — ADT variants always fully qualified

Evidence: variants are constructed and matched as `Shape.Circle` everywhere.

Problem: deep ADTs (`Expr.Binary.Op.Add`-shaped) get verbose; no grouping
mechanism exists.

Direction: leave construction qualified; allow match arms to omit the ADT
prefix when unambiguous within the matched type (type mode already knows the
scrutinee). Construction stays explicit.

Open question: ambiguity diagnostics when two nested ADTs share variant names.

### Goal-level gaps

#### F32 — C interop remains draft

Evidence: "FFI: C imports/exports and foreign ABI remain draft and are not
part of this language."

Problem: goals 8–9 ("Can import C code", "Trivial import of C libraries") are
unimplemented. Largest promise-versus-reality gap; honestly tracked under
RFC 0039.

Direction: none here — cross-reference only. This audit records that F4/F15/
F27-style API limitations matter less than they appear because C interop is
the intended escape hatch, raising 0039's priority accordingly.

Open question: none.

#### F33 — Residual no-UB leaks beyond F6

Evidence: "Unsynchronized conflicting access is a data race with no
guarantee"; "Exactly one successful join or detach is allowed across aliases"
(enforcement unstated); Mutex misuse "is programmer error" with trap only
"detectable from a live control block."

Problem: data races are honestly documented (acceptable — even Rust stops
short of preventing them without unsafe). But double-join/detach enforcement
is unspecified: trap, accept, or corrupt?

Direction: specify double-join/detach as a runtime trap via task-state
metadata (the scheduler owns the control block already). Document data-race
semantics as-is.

Open question: none.

#### F34 — Local-analysis memory checks have silent holes

Evidence: freeing pointers "copied to a second binding is not tracked";
"Interprocedural provenance from a caller argument is not checked"; "An
undecided case is always accepted."

Problem: the safe path is opt-in-by-luck — identical bugs live or die by
incidental shape. The reference documents this honestly, but goal 18's wording
("catches every memory error that a local analysis can decide") promises more
than the delivered set: the nested-View return case (F19) IS locally decidable
and missed.

Direction: fold into F19's fix; reword goal 18 in AGENTS.md to match the
delivered envelope ("catches every memory error whose decision requires only
single-function, no-escape analysis"), so the promise and the checker agree.

Open question: none.

## Disposition

Each finding resolves to exactly one of:

- **Promote**: a successor RFC carries the design. Recorded here by number.
- **Reject**: with rationale, so the same finding does not return.
- **Accept**: the cost is acknowledged and the current design stands.

| # | Disposition | Successor |
|---|---|---|
| F1 | Promote | TBD |
| F2 | Promote | TBD |
| F3 | Promote | TBD |
| F4 | Promote | TBD |
| F5 | Promote | TBD |
| F6 | Promote | TBD |
| F7 | Promote | TBD |
| F8 | Undecided | — |
| F9 | Undecided | — |
| F10 | Accept (provisional) | — |
| F11 | Undecided | — |
| F12 | Undecided | — |
| F13 | Undecided | — |
| F14 | Promote | TBD |
| F15 | Undecided | — |
| F16 | Undecided | — |
| F17 | Undecided | — |
| F18 | Accept (diagnostic-only) | — |
| F19 | Promote | TBD |
| F20 | Undecided | — |
| F21 | Accept (diagnostic-only) | — |
| F22 | Undecided | — |
| F23 | Promote | TBD |
| F24 | Undecided | — |
| F25 | Undecided | — |
| F26 | Accept (docs-only) | — |
| F27 | Undecided | — |
| F28 | Undecided | — |
| F29 | Undecided | — |
| F30 | Undecided | — |
| F31 | Undecided | — |
| F32 | Cross-reference | RFC 0039 |
| F33 | Promote | TBD |
| F34 | Fold into F19; AGENTS.md rewording | — |

Undecided entries await the author's call; this RFC does not decide them.

## Invariants

1. No finding here changes the language until its successor RFC closes.
2. The ten protected mechanisms listed under "What clicks" are not regressed
   by any successor RFC without naming this section.
3. Every successor RFC cites its finding number and must answer that finding's
   open question.
4. Rejected findings stay recorded with rationale; silence is not rejection.

## Validation

This section is exhaustive for THIS RFC (a findings catalog):

- Every finding quotes the reference text it challenges; a finding without a
  quote is invalid and is removed.
- Every finding names the goal it conflicts with or states "none" explicitly.
- The disposition table covers all thirty-four findings with no gaps.
- Reference quotations were verified verbatim against `docs/reference.md` at
  creation time; a quotation that no longer matches indicates the reference
  moved and the finding must be re-verified before promotion.
- Successor RFCs inherit validation obligations from their findings' open
  questions; this RFC adds no tests.

## Non-goals

- Deciding any Undecided disposition.
- Designing any promoted feature.
- Auditing compiler implementation quality, generated-C correctness, or
  performance (prior audits cover those).
- Changing `docs/reference.md` from this RFC alone.

## Drawbacks

- A findings catalog ages: reference movement silently invalidates quotes.
  The validation rule above turns staleness into a detectable condition.
- Thirty-four items invite cherry-picking; the severity table and disposition
  discipline exist to force whole-findings accounting.
