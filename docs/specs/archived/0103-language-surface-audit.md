# RFC 0103: Language Surface Audit

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; all findings re-audited and disposed 2026-08-27
- Created: 2026-08-21
- Updated: 2026-08-27
- Scope: syntax, semantics, and builtin API surface of the language as
  specified by `docs/reference.md`; evaluated against the project goals in
  `AGENTS.md`. Compiler implementation quality is out of scope.
- Coordinates with: `docs/reference.md`, `docs/status.md`, RFC 0039 (C interop),
  RFC 0027 (Arena/Pool)
- Companion: none. This RFC originates the findings; successor RFCs carry
  individual designs.

## Summary

Forty-six findings from a full read of the normative reference against the
stated goals: cohesive and intuitive surface, one obvious way to do things,
small clean surface, everything C can do, low ceremony, no undefined behavior,
the compiler catches every memory error a local analysis can decide.

The audit's overall verdict: the foundation mechanisms — ownership-decides-
representation, lossless-widening-only conversion, traps-not-UB arithmetic,
Error-in-unions with try/errdefer, structural union interning — are coherent
and worth protecting unchanged. Nearly every finding below is an omission or an
inconsistency between two otherwise-good rules, not a wrong mechanism.

Findings are numbered F1–F46 in one flat sequence, grouped by class. Each
finding states its evidence as quoted reference text, the goal it conflicts
with, a proposed direction, and the open question its successor RFC must
answer. The original finding text is historical evidence, not a current claim.
The 2026-08-27 disposition re-audit below governs every finding. F35–F46 were
added by the 2026-08-23 resurface audit.

## Severity ordering

| # | Finding | Class | Goal conflict | Size |
| --- | --- | --- | --- | --- |
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
| F35 | Same-line `(` call rule is invisible | lexical gap | intuitive | small |
| F36 | Module-path literal is a second string grammar | lexical asymmetry | one obvious way | small |
| F37 | `Byte` vs `UInt8` spelling rule is stylistic | surface weight | one obvious way | small |
| F38 | `Size` isolation forces explicit `to<Size>()` even for constants | ceremony | low ceremony | small |
| F39 | Object/Array literal evaluation order unspecified | underspecification | determinism posture | small |
| F40 | `Strand` has no `View` and no cached length | API hole | no runtime overhead | small |
| F41 | String literal vs runtime handle dual lifetime | safety hole | if it compiles, it runs | small |
| F42 | `print` arity and separator ceremony | API friction | low ceremony | small |
| F43 | `View.from_pointer` provenance is local-only | safety hole | catch memory errors | medium |
| F44 | `for` binder table is exact and rigid | ceremony | composability | small |
| F45 | `defer` discards fallible results; trap may skip cleanup | soundness | no undefined behavior | small |
| F46 | Task stack `Project` knobs leak target into language | surface weight | small clean surface | small |

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
component placement under the current generated-C naming and
component-ownership regime.

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

### Additional audit — 2026-08-23 resurface (F35–F46)

#### F35 — Same-line `(` call rule is invisible

Evidence: "`call-arguments = same-line , "(" , [ argument-list ] , ")"`"; "`call-statement = ? call-expression whose first token is identifier or \"self\" ?`"; diagnostics `"a call's ( must follow its callee on the same line"` and `"a return value must begin on the same line as return"`.

Problem: a newline silently changes `foo (bar)` from one call to two statements (`foo` then `(bar)` as parenthesized expression statement, which itself is rejected). The delimiter is whitespace, not a token, contradicting the otherwise token-delimited surface and the "no statement terminator" simplicity.

Direction: keep the same-line rule (it removes `;` and `\` continuations) but promote it in diagnostics/tutorial as a core delimiter like `do`/`then`; or allow an explicit continuation (trailing `\\`) for the rare split-call case. No change to `print`/`spawn` already left-to-right specified.

Open question: does the workbench rely on the newline break to separate a bare identifier statement from a following parenthesized expression?

#### F36 — Module-path literal is a second string grammar

Evidence: "`module-path-literal = ? a quoted literal scanned only when the previous token is \"import\" on the same line; the payload between quotes is taken verbatim (no escape decoding); a backslash in the payload is invalid ?`"

Problem: a second quoted form with raw verbatim rules (no `\\`, `\"`, `\n`) adds lexical surface for one import syntax; users expect `"./my\"lib"` escapes to work and get `invalid module-path literal` instead. Violates one obvious way for quoted text.

Direction: keep raw for portability (paths are identifiers, not text) and document as intentional; or unify with string literal and validate decoded payload as path. Raw is defensible — paths never need escapes — but the cost must be acknowledged.

Open question: should `module-path-literal` allow `\\` as path separator on Windows, or is `/` canonical and `\\` always an error?

#### F37 — `Byte` vs `UInt8` spelling rule is stylistic, not semantic

Evidence: "`Byte` is the canonical spelling wherever the value is raw storage rather than a number: `View<Byte>`, `Array<Byte, N>`, `List<Byte>`, and byte-oriented parameters and results. `UInt8` is canonical wherever the value is an 8-bit integer participating in arithmetic … Both remain the same canonical type; this rule governs spelling, not semantics (RFC 0063)."

Problem: one canonical type with two enforced spellings based on intent — a lint rule disguised as a language rule. `View<UInt8>` and `View<Byte>` are the same type but one is a style error. Violates one obvious way.

Direction: keep one type but downgrade spelling to `go vet`-level diagnostic or `fmt` rewrite, not a type error; or split into distinct `Byte` (non-arithmetic) and `UInt8` (arithmetic) types. Either is coherent; enforcement level is not.

Open question: does `Byte` arithmetic ban (`Byte` is `UInt8` but `Byte + Byte` is numeric) confuse the spelling rule's teaching?

#### F38 — `Size` isolation forces explicit `to<Size>()` even for constants

Evidence: "`Size` has no widening edges: no fixed-width integer or float implicitly converts to Size, and Size does not implicitly convert to any fixed-width integer or float, because no conversion is lossless on every conforming target."

Problem: `x: Size := 4` needs contextual literal typing (works) but `x: Size := y` where `y: Int32 = 4` needs `y.to<Size>()` with dynamic trap, even though `4` fits every target. In-range constants pay the same ceremony as out-of-range values. Low-ceremony goal hit at the most common `Size` use (`length`/`index`).

Direction: keep isolation for non-constant values (portable), but allow constant-only implicit path for values provably in `0..SIZE_MAX` on all targets via `static_assert`; or keep explicit and accept ceremony. Isolation is honest about target variance.

Open question: does constant-only widening reintroduce target-dependent typing that `Size` was designed to hide?

#### F39 — Object and array literal evaluation order is unspecified

Evidence: "Initializer evaluation order is unspecified." "Unless stated otherwise, operand order, call-argument order, receiver-versus-argument order, and object-initializer order are C23-unspecified."

Problem: same underspecification as F23 but for construction: `Point{x = foo(), y = bar()}` and `[a(), b()]` have unspecified order, while `print` and `spawn` are specified left-to-right. Side-effecting initializers have determinism hole.

Direction: extend F23's proposed left-to-right guarantee to object-member initializers and array elements. Generator already sequences multi-step lowerings; formalize it.

Open question: does sequencing object initializers in source order or declaration order match user expectation when literal order differs from declaration order?

#### F40 — `Strand` has no `View` and no cached length

Evidence: "`Strand` is immutable literal-only inline 32 bytes: at most 31 UTF-8 bytes … Strand has no room for a count and scans, bounded by its 31 payload bytes. … Neither is indexable … `Strand` exposes no View into inline bytes."

Problem: `Strand` cannot be sliced or viewed without `to_string(heap)` allocation (`Strand.to_string(heap) -> String` then `String.bytes()`). Iterating runes via `rune_cursor` is gone (now on `String` only). Zero-cost literal story forces heap for read-only window.

Direction: add `Strand.bytes() -> View<Byte>` borrowing inline storage (like `String.bytes()`) or `Strand.slice`, or document as intentional small loss to keep `Strand` 32 bytes. F5's `Strand/String` comparison fix may subsume part of this.

Open question: does a `Strand` View extend lifetime beyond the literal's inline storage in a way that breaks the borrowing model?

#### F41 — String literal vs runtime handle dual lifetime

Evidence: "Runtime values use one header-plus-bytes allocation; literals use static storage." "Runtime String allocations require one matching free; all aliases then dangle. Literals must never be freed."

Problem: one type `String` has two lifetimes with different `free` obligations; `if (isLiteral) skip free` is manual and error-prone. Violates ownership-decides-representation uniformity where `String` is supposed to be one handle shape.

Direction: keep dual storage but make `free` on a literal a checked trap (or no-op) instead of undefined; or add `String.is_literal` / separate `StaticString` type. Current `must never be freed` is a trap waiting to happen.

Open question: does the generator already emit a runtime check to trap `free` of a literal, or is it truly UB?

#### F42 — `print` arity and separator ceremony

Evidence: "`print(first: Printable, rest: Printable...) -> no value` is protected, requires at least one argument, inserts no separator/newline, and returns no value."

Problem: `print()` with zero args is invalid, every line needs manual `"\n"` and `", "`; depth-dependent quoting (F24) plus arity makes the most common teaching example `print("hello\n")` carry ceremony for no reason. Low-ceremony goal hit.

Direction: allow zero-arg no-op or add `println(...)` as F24 proposes; keep single-arg minimum if intentional for explicitness. No separator change needed.

Open question: does `println` earn its surface weight as a second protected form, or should `print` with trailing newline be the idiom?

#### F43 — `View.from_pointer` provenance is local-only

Evidence: "`View<T>.from_pointer(pointer: Ptr<T> | MutPtr<T>, length: Size) -> View<T>` … It rejects pointers locally traceable to `ref` and accepts heap or opaque parameter pointers. Interprocedural provenance from a caller argument is not checked."

Problem: `ref local` passed through `fn f(p: Ptr<Int32>) -> View<Int32> { return View<Int32>.from_pointer(p, 1) }` compiles, reintroducing a dangling-View hole similar to F19/F34 but for `View` construction. Local-only check is honest but holes remain.

Direction: extend tracking through one indirection (parameter `Ptr` that is itself `ref`-derived in caller) or document as accepted hole like F34. Bounded by single-function analysis, not full escape analysis.

Open question: false-positive rate if any `Ptr` parameter is conservatively assumed `ref`-derived?

#### F44 — `for` binder table is exact and rigid

Evidence: "| Array, View, List, String, Strand | 1 | value | … | 2 | `index: Size`, value | … Dict 2 key,value 3 index,key,value Every other source/arity combination is invalid."

Problem: cannot ignore value and take only index (`for i, _ in list`), or take index with Dict's 2-form; `for v in dict` is invalid, must write `for k,v in dict`. Uniform but rigid; `_` discard not defined for binders.

Direction: allow `_` as a discard binder in any position, or allow trailing binders to be omitted (`for i in list` gives index only). Either keeps exact typing.

Open question: does `_` as binder need a reserved-word or is it an ordinary identifier discard?

#### F45 — `defer` discards fallible results; trap may skip cleanup

Evidence: "Cleanup result values are discarded. Process traps need not run cleanup." "Only `IO.close()` may appear in defer/errdefer." "`defer expression` … A direct call captures callee, receiver, and arguments at registration"

Problem: `defer io.close()` returns `Nil|Error` but error is discarded; failure is silent. Trap path (`[Runtime Error]`) need not run `defer`, so `List.free` in `defer` may leak on trap. Soundness/ceremony tension.

Direction: keep discard (cleanup must not fail) but lint `defer` of `Nil|Error` without handling; document trap-skip as intentional (traps are fatal). Or allow `errdefer` for root cleanup.

Open question: should `defer` of `IO.close()` be required to handle `Error`, or is discard the intended semantics for close-during-unwind?

#### F46 — Task stack `Project` knobs leak target into language

Evidence: "Stacks reserve 1 MiB by default with an 8 KiB initial commit, both `Project` build-time settings; the initial commit is a Windows-only knob, and the usable region is the reserve less one guard page."

Problem: language-level `Project` carries OS page-commit knob (`TaskStackCommit` Windows-only). Breaks small-clean-surface and target-agnostic language story; `Project` should be language settings, not OS tuning.

Direction: keep in `Project` but hide Windows-only knob behind `target` profile (RFC 0052) or default to reserve-only and remove commit knob; document commit as Windows-only explicitly (already) and accept.

Open question: does the POSIX guard-page implementation need a commit knob at all, or can it be removed?

## 2026-08-27 re-audit evidence

Focused in-memory probes against the current compiler produced:

```text
Array<UInt8, 1> and View<UInt8>       exit=0
nested local-rooted View return       exit=0
String literal followed by free       exit=0
String == Strand                      exit=1; identical canonical types required
Dict<UInt64, Int32>                   exit=1; key must be Int32 or Strand
```

The first result invalidates F37. The next two reproduce the safety defects
promoted to RFCs 0137 and 0138. The final two confirm that F5 and F4 remain
documented feature restrictions rather than implementation drift. Reference
and implementation inspection separately verified the resolved findings in
the disposition table.

## Disposition

Each finding resolves to exactly one of:

- **Promote**: a successor RFC carries the design. Recorded here by number.
- **Reject**: with rationale, so the same finding does not return.
- **Accept**: the cost is acknowledged and the current design stands.
- **Resolved**: current code and reference already close the finding.
- **Invalid**: the reported behavior does not reproduce.

| # | Disposition | Current decision or successor |
| --- | --- | --- |
| F1 | Resolved | Module-level functions and methods now support forward and mutual recursion. |
| F2 | Promote | RFC 0135; scalar value match is a substantial switch-like language feature, not a bug. |
| F3 | Reject | `match` already provides conditional values; a second conditional-expression form violates the one-obvious-way goal. |
| F4 | Promote | RFC 0136; expanded Dict keys require hashing and representation design. |
| F5 | Promote | RFC 0139; mixed String/Strand comparison removes an otherwise mandatory allocation. |
| F6 | Resolved | List and Dict carry mutation versions; traversal rejects provable mutation and traps live invalidation. |
| F7 | Reject | Root scope has no Error result to receive propagation; explicit `match` keeps process policy visible. |
| F8 | Accept | Lua-like truthiness is deliberate, uniform, and precisely limited to `false` and `nil`. |
| F9 | Accept | `Task.yield()` is a protected builtin namespace operation, not a user-defined static-method facility. |
| F10 | Resolved | EoS now consistently represents end-of-sequence for Channel and IO reads; the one-use premise is stale. |
| F11 | Reject | `== nil` is the sole null test; adding `is Nil` would create a synonym without new capability. |
| F12 | Reject | Rune is a Unicode scalar, not an arithmetic integer; explicit conversion preserves intent and scalar validation. |
| F13 | Reject | Literal-specific escape sets protect UTF-8 and scalar validity; uniform byte escapes would weaken those contracts. |
| F14 | Reject | Nested block comments add lexer state and little capability; line comments and non-nested block comments suffice. |
| F15 | Accept | Atomic's allowlist reflects supported lock/representation contracts; broaden only for a concrete use and verified targets. |
| F16 | Reject | `first() -> T | Nil` is ambiguous when T contains Nil; `length()` plus checked indexing is explicit and complete. |
| F17 | Resolved | Heap is now a stateless default-allocation token; the stored-allocator identity premise no longer exists. |
| F18 | Accept | Narrowing remains direct-local flow analysis; aliases require an ownership/alias model rather than ad hoc propagation. |
| F19 | Promote | RFC 0137 fixes inline aggregates; RFC 0110 owns mutable List/Dict element lifetimes and aliasing. |
| F20 | Accept | The literal-loop check is a bounded syntactic guarantee, not a claim to decide arbitrary starvation. |
| F21 | Accept | The generic-angle heuristic is deterministic and fails with syntax diagnostics; no reproduced ambiguity remains. |
| F22 | Accept | Parentheses explicitly disambiguate a union type pattern from match-arm separators. |
| F23 | Resolved | The reference now specifies receiver, argument, operand, initializer, and assignment evaluation order. |
| F24 | Reject | Existing print spelling and quoting are deterministic; separator/newline variants add convenience surface only. |
| F25 | Accept | Error remains a flat value; recursive causes would add allocation, ownership, formatting, and cleanup policy. |
| F26 | Resolved | Function values are now returnable and storable in aggregates and collections subject to their explicit exceptions. |
| F27 | Cross-reference | RFC 0117 owns all restricted compile-time expressions, including future computed Array lengths. |
| F28 | Reject | One method per `impl` keeps declarations uniform and avoids a second grouped spelling. |
| F29 | Reject | Repeated arms are explicit; alternation adds pattern grammar without adding expressiveness. |
| F30 | Reject | Labeled loops are low-ROI surface; flags, functions, and returns already express the control flow. |
| F31 | Reject | Qualified variants preserve nominal clarity and avoid import-dependent lookup ambiguity. |
| F32 | Cross-reference | RFC 0039 owns compiler-core C interoperability. |
| F33 | Resolved | Iterator invalidation and Task join/detach terminal claims now close the reproduced no-UB cases. |
| F34 | Resolved | The project goal and reference now state the exact local-analysis boundary and its deliberate limitations. |
| F35 | Accept | Same-line call syntax is a deliberate lexical boundary with explicit diagnostics. |
| F36 | Accept | Import paths are logical source-map keys with a small `/`-only grammar independent of host filesystems. |
| F37 | Invalid | `Array<UInt8, N>` and `View<UInt8>` compile; Byte is a contextual canonical spelling, not a type restriction. |
| F38 | Reject | Size stays target-dependent and isolated; contextual integer literals already avoid unnecessary conversion ceremony. |
| F39 | Resolved | Folded into the complete evaluation-order contract that resolves F23. |
| F40 | Reject | Strand-to-View would introduce borrowing and lifetime rules for negligible benefit over its bounded 32-byte operations. |
| F41 | Promote | RFC 0138; freeing a literal String currently reaches the heap deallocator and is a runtime safety bug. |
| F42 | Reject | Same decision as F24; variadic separator policy and `println` are convenience APIs, not missing primitives. |
| F43 | Accept | `View.from_pointer` keeps function-local provenance; caller propagation requires interprocedural borrow analysis. |
| F44 | Reject | The exact binder table is small and unambiguous; `_` would add discard-binding syntax solely for convenience. |
| F45 | Accept | `defer` is cleanup on language exits, discarded cleanup results are explicit, and fatal runtime traps do not unwind. |
| F46 | Reject | `Project` contains build-time target settings and is not part of the Hexal language surface. |

## Invariants

1. No finding here changes the language until its successor RFC closes.
2. The ten protected mechanisms listed under "What clicks" are not regressed
   by any successor RFC without naming this section.
3. Every successor RFC cites its finding number and must answer that finding's
   open question.
4. Rejected findings stay recorded with rationale; silence is not rejection.
5. Resolved and invalid findings were checked against the 2026-08-27 tree and
   normative reference before this catalog closed.

## Validation

This section is exhaustive for THIS RFC (a findings catalog):

- Every finding quotes the reference text it challenges; a finding without a
  quote is invalid and is removed.
- Every finding names the goal it conflicts with or states "none" explicitly.
- The disposition table covers all forty-six findings with no gaps.
- Every finding has a terminal disposition, named successor, or active owner.
- Resolved claims and the invalid F37 claim were re-probed or verified against
  the current reference and tree on 2026-08-27.
- Successor RFCs inherit validation obligations from their findings' open
  questions; this RFC adds no tests.

## Non-goals

- Designing any promoted feature.
- Auditing compiler implementation quality, generated-C correctness, or
  performance (prior audits cover those).
- Changing `docs/reference.md` from this RFC alone.

## Drawbacks

- A findings catalog ages: reference movement silently invalidates quotes.
  The validation rule above turns staleness into a detectable condition.
- Thirty-four items invite cherry-picking; the severity table and disposition
  discipline exist to force whole-findings accounting.
