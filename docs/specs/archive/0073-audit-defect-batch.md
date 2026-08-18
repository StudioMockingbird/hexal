# RFC 0073: Audit Defect Batch

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-18. All seventeen defects are fixed,
  the D8–D13/D28–D32 latent fail-open paths are swept, and the D14–D16/D18
  documentation contradictions are resolved with `docs/reference.md`.
  D19/D25, the last item, shipped as `Type.CanonicalKey` (recursive,
  module-qualified identity; display names never participate) plus a
  per-compilation `types.Arena` shared by every module environment, with
  collection C-name disambiguation on collision; integration coverage lives
  in `modules_identity_test.go`. D31, the sole originally-unverified defect,
  is confirmed not reproducing: `compiler/tests/c23validation/` has no `.at(`
  and every `try` sits in an Error-returning function. D2 and D26 are fixed
  individually as ordinary defects; the toolchain-backed validation gate for
  the uncompilable-C class stays deferred by the decision in Readiness.
- Created: 2026-08-16
- Updated: 2026-08-18
- Scope: correctness defects found by the six-pass refactor audit — compiler
  behavior, generated C, and normative-document contradictions
- Coordinates with: RFC 0074 (refactor batch), `AGENTS.md`, `docs/reference.md`,
  `docs/status.md`
- Companion: RFC 0074 carries every non-defect finding from the same audit

## Summary

Seventeen live defects, eleven latent fail-open paths, and four documentation
contradictions. Four independent audits — an internal six-agent pass, an
external Codex pass, and two OpenCode passes — contributed findings; D1 and D6
were reported by both of the first two, and D33 came from the review of RFC
0072. One reported contradiction (D17) was withdrawn as a false finding and one
reported defect (union canonicalization) failed to reproduce; both are recorded
rather than deleted.

Every live defect in this RFC was reproduced by an independent probe, not
inferred from reading code. Probe programs and observed output are recorded per
item so a fix can be verified against the same evidence. Findings that did not
reproduce are recorded under "Excluded after verification" rather than dropped.

## Severity ordering

| # | Defect | Class | Effort |
|---|---|---|---|
| **D19** | **Generic specialization keys collide across modules** | **silent miscompile** | **high** |
| **D25** | **The same builtin generic has different identities per module** | **valid programs rejected** | **high** |
| D23 | UTF-8 validation accepts invalid encodings | unsound text | medium |
| D26 | `Atomic<T>` binding discards `const` in generated C | uncompilable C | low |
| D27 | Generator validation rejects `for`/`errdefer` in generic bodies | valid programs rejected | low |
| D2 | Handle types reachable only through a declaration emit uncompilable C | miscompile | medium |
| D33 | Size-only unsigned arithmetic emits `uint64_t` without `<stdint.h>` | uncompilable C | low |
| D1 | Top-level `for` rejected as `Unknown Error` | valid program rejected | 1 line |
| D20 | `eos == eos` rejected despite EoS equality being specified | valid program rejected | low |
| D21 | Imports after declarations are accepted | grammar not enforced | low |
| D22 | Deferred call through a `Fun<>` value fails in generation | valid program rejected | medium |
| D3 | `try` and `spawn` accepted inside `errdefer` | miscompile | 2 lines |
| D4 | `from_pointer` provenance defeated by one copy | unsound View | low |
| D24 | Empty List/View slice performs null-pointer arithmetic | UB in generated C | low |
| D5 | Private type escapes when an unrelated module exports its name | visibility hole | low |
| D6 | Generated C spells `NULL` | conformance | 10 min |
| D7 | `Stats.ParseDuration` never assigned | wrong number shown to users | 15 min |
| D8–D13, D28–D32 | Latent fail-open paths | unreachable today | small each |
| D14–D16, D18 | Normative-document contradictions | documentation | minutes |

## D19 — Generic specialization keys collide across modules

**The most severe defect in this batch. It compiles cleanly and corrupts memory
layout at runtime.**

`specializeKey` (`compiler/checker/generics.go:97`) builds the specialization
identity from `Type.Name`. Nominal types carry their defining module's identity
(`docs/reference.md`: "same-named types in different modules are distinct"), but
the *display name* does not. Two modules that each export a `Point` therefore
produce one specialization key.

Observed, with two structurally different types:

```hexal
// m.hex
export type Point = { x: Int32 }            // 4 bytes
// s.hex
export type Point = { y: Int64, z: Int64 }  // 16 bytes
// app.hex
a: List<m.Point> = List<m.Point>.new(h)
b: List<s.Point> = List<s.Point>.new(h)
```

```
exit = 0, no diagnostics
hexal/list.h contains exactly ONE typedef:  } hex_list_Point;
```

Both lists share a single C type. Element size, index arithmetic, `push`, and
every copy are wrong for one of them. Nothing reports it — the program compiles,
links, and runs with a corrupted layout.

Codex's audit reported the same omission in union canonicalization. **It does
not reproduce — see Excluded after verification.** Unions are already correctly
discriminated; do not write a union fix.

### Fix

Introduce one stable, recursive, module-qualified canonical type key, and use it
for every specialization identity. Display names must never participate in
identity. `EncodeModuleOwner` (`compiler/types/types.go`) already
produces the length-delimited module encoding used for C symbols — the key
should be built from the same source of truth.

### Test

- Two modules exporting same-named types with **different layouts**, used as
  `List<T>`, `Dict<K,V>`, `Array<T,N>`, and `View<T>`: two distinct
  specializations, two typedefs, correct `sizeof` for each.
- Same-named types with **identical** layouts still produce two specializations
  — identity is nominal, not structural.
- The same pair as union members.
- A single-module control confirming no duplicate specialization is introduced.

RFC 0074 records the canonical-key architecture; this RFC owns the defect. The
key must land here, not be deferred to the refactor batch.

## Resolved design — type identity

D19 and D25 share a subject but **not** a cause, and each can be fixed
independently. The code makes both mechanisms explicit.

`BeginObject` (`compiler/types/types.go:369-375`) already produces a
module-qualified C name and a bare everything-else:

```go
cName := "hex_t_" + SanitizeIdentifier(name)
if environment.owner != "" {
    cName = "hex_t_" + environment.owner + "_" + SanitizeIdentifier(name)  // qualified
}
identity.signature = "object:" + name                                       // bare
object := &ObjectType{ Name: name, ... }                                    // bare
```

**D19 is `Name` used as identity.** Both generic paths key on it:

```go
// checker/generics.go — user generics
names[index] = argument.Name                                  // two Points → one key

// types/collections.go:154 — builtin generics
CName: "hex_list_" + SanitizeIdentifier(element.Name),        // two Points → one hex_list_Point
```

**D25 is `Environment` being per-module.** `ListType` interns on an
environment-scoped serial, and `NewEnvironmentWithOwner` gives every module a
fresh identity root:

```go
key := "list:" + strconv.FormatUint(element.identity.serial, 10)
identity := newTypeIdentity(environment.identity)             // scoped to this module
```

### Decision 1 — identity is a third field, not a reused string

Add `CanonicalKey` to `Type`. Three fields, one job each:

```go
type Type struct {
    Name         string  // "Point"              — diagnostics only
    CName        string  // "hex_t_m1_m5_Point"  — generated C only
    CanonicalKey string  // "m1_m5:Point"        — identity only, never displayed
}
```

`specializeKey` and every collection constructor key off `CanonicalKey`. It is
built recursively: a constructed type's key composes its constructor and its
arguments' keys, so `List<m.Point>` and `List<s.Point>` differ at the top level.

**`specializeTypeName` is not an identity site and must stay unqualified.** It
renders display names — `Box<Int32>` — for diagnostics, and `CanonicalKey` is
"identity only, never displayed" by the table above. Keying it off the canonical
key would leak module encodings into user-facing messages. Its sibling
`specializeFunctionName` renders a C-name *stem* and is likewise unqualified:
module qualification is applied downstream, which is why two modules each
exporting a generic `identity<T>` already emit distinct symbols today
(`hex_f_m1_a_identity_Int32` and `hex_f_m1_b_identity_Int32`, verified). Three
neighbouring functions, three different jobs — identity, display, C stem — and
only the first changes.

Rejected: reusing `CName` as the key. It is unique and already qualified, so it
works — but it makes one string serve two purposes, which is the defect being
fixed, and it forces collection C names to
`hex_list_hex_t_m1_m5_Point`. Rejected: qualifying `identity.signature` and
keying off that — smallest diff, but it promotes free-form debug text to a
load-bearing contract.

`docs/reference.md` already states display names must never identify types. This
is the change that makes that structurally true rather than aspirational.

### Decision 2 — split `Environment` into a per-compilation arena and a per-module scope

Constructed types — pointer, nullable, fun, array, view, list, dict, task,
channel, atomic, union — intern once per **compilation**. Names, aliases, and
generic declarations stay per **module**.

Rejected: one `Environment` for the whole compilation — module-scoped names would
then collide, so it trades D25 for a name-resolution bug. Rejected: making
`Equal` structural for constructed types — it replaces an O(1) pointer compare
with a recursive walk on the checker's hottest path and leaves duplicate type
objects alive for the whole compilation.

**This work moves here from RFC 0074 R20.** That RFC deferred the arena as a
retention and isolation improvement; D25 makes it a correctness requirement, and
the two cannot be done separately. RFC 0074's Stage 7 records the move.

### Decision 3 — generated C symbol names do not change

Keep deriving collection and specialization C names from `Name` where no
collision exists. Disambiguate **only on collision**, appending the module
encoding to the second and subsequent claimants in a deterministic order.

**Collision resolution is one program-wide resolver, consulted by every
derivation site — never a per-site append.** A bare element name reaches many
derivations: the element typedef, the `push`/`at`/`slice` helper suffixes, the
paired View type, component selection keys, and wrap helpers. If the typedef
site resolves a collision and a helper site derives from the bare name, the pair
desynchronizes and the typedef no longer matches the helper that operates on it
— an uncompilable-C failure of exactly the D2/D33 class, and one no ordinary
test can see. Resolve once, ask everywhere.

Every existing program's generated C stays byte-identical, the snippet manifest
moves only for genuine collision cases, and the diff is attributable to the
defect rather than to the fix.

Rejected: deriving all C names from `CanonicalKey`. Correct, but it rewrites
every artifact and buries the actual fix in noise.

## D25 — The same builtin generic has a different identity in each module

**D19 and D25 are one root cause seen from opposite sides.** D19 is
under-discrimination: two genuinely distinct types collapse into one identity.
D25 is over-discrimination: one type splits into several. Both come from type
identity not being canonical and module-qualified in exactly one place, and both
should be fixed by the same canonical key.

`NewEnvironmentWithOwner` (`compiler/types/types.go:271`) interns constructed
types per module, and `Equal` (`:590-595`) compares identity only. So
`List<Int32>` built in module A is a different identity from `List<Int32>` built
in module B.

Observed:

```hexal
// lib.hex
export fun take(v: List<Int32>): Nil | Error do return nil end
// app.hex
l: List<Int32> = List<Int32>.new(h)
lib.take(l)
```
```
rejected: [Type Error] take argument 1 requires List<Int32>, got List<Int32>
```

The diagnostic says a type does not match itself. Passing a `List` across a
module boundary — an entirely ordinary thing — cannot be done, and the error
message gives the user nothing to act on.

The generator is incidentally immune because `mergeTypeOrders`
(`compiler/generator/emission.go:501`) deduplicates by C name, but the checker
rejects first, so that never matters.

### Fix

One canonical, module-qualified, per-compilation type identity shared by every
module environment — the same key D19 requires. Builtin and constructed generic
types must intern once per compilation, not once per module.

### Test

- A `List`, `Dict`, `Array`, and `View` passed across a module boundary as a
  parameter, a return, an object member, and a generic argument.
- A user generic function in module A called with a container built in module B.
- D19's collision cases in the same run, since one key must satisfy both
  directions.

## D26 — An `Atomic<T>` binding discards `const` in generated C

An immutable `Atomic<T>` binding renders as `const`, but the generated accessor
takes a non-const pointer.

Observed for `counter: Atomic<Int32> = Atomic<Int32>.new(0)` followed by
`counter.fetch_add(1)`:

```c
const hex_atomic_Int32 hex_v_counter = hex_atomic_Int32_new(0);
const int32_t hex_v_v = hex_atomic_Int32_fetch_add(&(hex_v_counter), 1);

static inline int32_t hex_atomic_Int32_fetch_add(hex_atomic_Int32 *atomic, int32_t value)
```

`&(hex_v_counter)` has type `const hex_atomic_Int32 *` and is passed to a
`hex_atomic_Int32 *` parameter. That discards a qualifier — a constraint
violation, rejected under `-Werror` and warned by default on both GCC and
Clang. The same applies to `store`, `load`, `exchange`, and `fetch_sub`.

It also violates `docs/reference.md`'s C23 output contract directly: "No
qualifier-discarding cast is emitted."

`compiler.Compile` returns exit 0 with no diagnostics, and the existing
`TestAtomicOperationsCompile` passes. **This is the second concrete instance of
the gap in RFC 0074 R18: green Go tests, uncompilable C.**

### Fix

Either emit the binding without top-level `const` when the type is `Atomic<T>`
— consistent with `RuneCursor`, which `docs/reference.md` already exempts as
"mutable-through" — or give the accessors const-correct signatures. The first
matches the existing precedent.

### Test

Assert the generated declaration for an immutable `Atomic` binding carries no
qualifier that its accessors reject, for every operation in the `Atomic` set.

## D27 — Generator validation rejects `for` and `errdefer` inside generic bodies

`validateStatements` (`compiler/generator/validation.go:112-267`) has no case for
`ForStatement` or `ErrdeferStatement`, so both reach the fail-closed default at
`:265`.

Observed:

```
for inside generic fun        rejected: [Unknown Error] unsupported checked statement
errdefer inside generic fun   rejected: [Unknown Error] unsupported checked statement
control: for inside plain fun ACCEPTED
```

The control case proves the omission is specific to the path generic
specialization takes through generator validation. A generic function whose body
contains an ordinary `for` loop cannot be compiled, and the failure again blames
the compiler rather than reporting a user error.

Distinct from D1, which is the *checker's* top-level dispatch. Both are missing
`ForStatement` cases in different switches; fix them together and test both.

### Fix

Add both cases to `validateStatements`.

### Test

Generic functions containing `for`, `errdefer`, `defer`, `while`, `break`, and
`continue`, each specialized at two argument types.

## D23 — UTF-8 validation accepts invalid encodings

`hex_utf8_next` (`compiler/generator/packages/string.c:5`) selects a width from
the lead byte and then verifies only *continuation shape*
(`(byte & 0xC0) != 0x80`). It never validates the decoded scalar.

Accepted today, all invalid:

| Input class | Why it passes |
|---|---|
| **Bare continuation byte as a lead** (`0x80`–`0xBF`) | the second branch is `lead < 0xE0`, so the whole range `0x80`–`0xDF` is treated as a 2-byte lead |
| **Overlong encodings** (`C0 80`, `E0 80 80`) | no minimum-scalar check |
| **Surrogates** (`ED A0 80` → U+D800) | no surrogate-range check |
| **Above U+10FFFF** (`F4 90 80 80`) | no maximum-scalar check |
| **`F5`–`F7` leads** | the fourth branch is `lead < 0xF8`, admitting leads that cannot encode a valid scalar |

`docs/reference.md` states String is immutable UTF-8, that `from_bytes`
"validates before allocation and traps on malformed UTF-8", and that Rune
excludes surrogates. None of those hold.

The bare-continuation-byte case is not in Codex's report; it was found while
verifying it.

### Fix

Validate the decoded scalar, not only the byte shape: reject leads in
`0x80`–`0xC1` and `0xF5`–`0xFF`, enforce the minimum scalar per width, reject
`U+D800`–`U+DFFF`, and reject above `U+10FFFF`.

### Test

A boundary table with accept and reject cases for each class above, plus the
shortest valid encoding at each width and the exact boundary pairs
(`U+D7FF`/`U+D800`, `U+DFFF`/`U+E000`, `U+10FFFF`/`U+110000`).

## D20 — `eos == eos` is rejected

`docs/reference.md` lists EoS among the types that "compare by value", but
`inferExpressionType` (`compiler/checker/operator_checking.go:831`) omits
`EosLiteral`.

Observed:

```
eos == eos   rejected: [Type Error] unsupported expression
                     | [Type Error] unknown variable b at 2:7
```

Two problems: the comparison is rejected, and the diagnostic is a
dispatch-default `"unsupported expression"` with no position, which then
cascades into a spurious second error about the binding it failed to type.

### Fix

Add `EosLiteral` to the inference cases. The cascading second diagnostic should
disappear with it; verify that it does.

## D21 — Imports after declarations are accepted

`docs/reference.md` requires imports to precede all other items. The parser
permits a `type`, `fun`, or `impl` declaration to appear before an import
without ending the import prefix.

Observed:

```
type T = { n: Int32 }
module a = import "./a"      ACCEPTED
```

### Fix

End the import prefix at the first non-import top-level item.

### Test

An import after each of a type declaration, a function declaration, an impl
declaration, and an executable statement — all rejected with a positioned
diagnostic. Imports-only-first programs remain accepted.

## D22 — A deferred call through a `Fun<>` value fails during generation

The checker accepts calling a function value. `renderDeferredCall`
(`compiler/generator/defer.go:219`) accepts only named functions and a fixed
list of methods.

Observed:

```hexal
callback: Fun<(Int32): Nil | Error> = cleanup
defer callback(1)
```
```
rejected: [Unknown Error] deferred call without a checked function callee
```

Same class as D1: the checker accepts the program, generation reports the
*compiler* is broken, and there is no position. `docs/reference.md` places no
restriction on the callee form of a deferred call.

### Fix

Represent a deferred call as a captured ordinary call plan and reuse normal call
lowering, rather than re-implementing a callee whitelist. Capture-at-registration
semantics for the callee, receiver, and arguments must be preserved exactly.

### Test

`defer` and `errdefer` through a `Fun<>` binding, a `Fun<>` parameter, and a
`Fun<>` object member; plus the existing named-function and method forms as
regressions.

## D24 — Empty List/View slicing performs null-pointer arithmetic

`compiler/generator/packages/list.h:91` and `view.h:20` both end with:

```c
return (ViewT){&data[start], end - start};
```

When the backing pointer is null — an empty List, or a recursively sliced empty
View — `&data[0]` is null-pointer arithmetic, which is undefined behavior in C
regardless of the resulting value being unused.

### Fix

```c
data == nullptr ? nullptr : &data[start]
```

### Test

Slicing an empty List, an empty View, and a slice of an empty slice; each
yields a zero-length view without forming the address.

## D1 — Top-level `for` is rejected as a compiler bug

`checkModule`'s top-level dispatch (`compiler/checker/checker.go:343-475`) handles
15 of 16 `TopLevelItem` implementations. `parser.ForStatement` is absent, so it
falls through to the fail-closed default at `:469`.

Observed:

```
top-level for     exit 1: [Unknown Error] unsupported top-level item
for inside fun    OK
top-level while   OK
top-level if      OK
```

Three contract violations at once:

- **A valid program is rejected.** `docs/reference.md:46` makes `for-statement`
  a `statement`, and `statement` a legal `top-level-item`.
- **The category is wrong.** `Unknown Error` means "an unclassifiable compiler
  inconsistency, not a source-program error" (`docs/reference.md:1096`,
  `AGENTS.md`). The compiler tells the user it is broken when the user's program
  is correct.
- **No position.** The default arm constructs a diagnostic with no `Line` or
  `Column`.

`executableItemToken` already lists `ForStatement` as valid, `checkStatement`
already handles it (`control_flow.go:91`), and `walkProgram` already visits it.
Only the dispatch arm is missing.

### Fix

Add `case parser.ForStatement` beside the existing `IfStatement` and
`WhileStatement` arms, which already delegate to `checkStatement`.

### Test

Top-level `for` over an `Array`, a `List`, and a `String` compiles and emits the
same loop body as the identical loop inside a function.

## D2 — Handle types reachable only through a declaration emit uncompilable C

Concurrency discovery is expression-driven (`compiler/generator/concurrency.go:95-172`).
The type walkers (`compiler/generator/walk.go:53-134`, `:245-328`) descend Union,
Ptr, Array, View, List, Dict, Signature, and Object payloads but never `Task`,
`Channel`, `Mutex`, or `Atomic` payloads.

A program that names a handle type without performing a handle operation
therefore selects no concurrency component.

Observed for `fun consume(c: Channel<Int32>): Int32 do return 1 end`:

```
exit = 0, no diagnostics
hexal.h        = 63 bytes, does NOT mention hex_channel
modules/app.c  = DOES reference hex_channel
no concurrency component artifact emitted
```

The generated C references an undeclared type. It cannot compile.

This violates "if it compiles, it runs", and `AGENTS.md`'s rule that unsupported
input must produce a structured diagnostic rather than silently incomplete
output. Confirmed for a `Channel<T>` parameter; the same shape is expected for a
`Task<R>` parameter, a `Channel<T>` return, a `Mutex` parameter, and an
`Atomic<T>` object member — enumerate and test all five.

### Fix

Extend type-tree descent to handle payloads so declaration-only reachability
selects the concurrency component, exactly as it already does for List, Dict,
View, and Array.

### Test

For each of the five shapes: the concurrency component artifact is emitted, and
the artifact set is identical to the same program that also performs a handle
operation.

**This defect is the strongest argument for RFC 0074's generated-C compilation
gate.** `go test ./...`, `go vet ./...`, and `go vet -tags c23` all pass on the
program above.

## D33 — Size-only unsigned arithmetic emits `uint64_t` without `<stdint.h>`

The mirror image of D2. There, expression-driven discovery misses a type; here,
type-driven discovery misses an expression.

`collectTypeRequirements` (`compiler/generator/emission.go:1044-1061`) selects
headers from the types a program *spells*: `IsSize` selects `<stddef.h>` and
returns, checked before `IsInteger` because Size is also an unsigned scalar.
`renderUnsignedArithmetic` (`compiler/generator/render.go:1706-1709`) then
picks a *rendered* intermediate type by width — `uint32_t` for UInt8/UInt16,
`uint64_t` for everything else, Size included. No type in the program spells
`uint64_t`, so nothing selects the header that declares it.

Observed for `a: Size = 1  b: Size = 2  c: Size = a + b`:

```
exit = 0, no diagnostics
hexal.h        = #include <stddef.h>          — and nothing else
modules/app.c  = const size_t hex_v_c = (size_t)((uint64_t)hex_v_a + (uint64_t)hex_v_b);
```

`uint64_t` is undeclared. The generated C cannot compile.

Size is the only affected type: every other unsigned type spells an exact-width
C name and selects `<stdint.h>` through the `IsInteger` arm. Found by an
external review of RFC 0072 and confirmed against current `main` — 0072 does not
introduce it and must not inherit it.

### Fix

Narrow, by decision: emitting an unsigned arithmetic intermediate registers
`<stdint.h>`, whatever the operand type spells. Rendering, not spelling, is what
creates the dependency. This closes exactly the observed case.

The general fix — every renderer that emits a C type name registers that type's
header — is the root-cause form and is **deliberately not taken here**. It
touches every render site, and the number of sites that spell a type name
outside the requirement walker is currently unknown, so its blast radius is
unbounded relative to one undeclared identifier. Revisit if a third instance of
this class appears; two (D2, D33) were each found by a different external
review, so the class is not yet closed.

### Test

A Size-only arithmetic program asserts `#include <stdint.h>` is present in
`hexal.h`. This must be a **textual** assertion. No test invokes a toolchain and
the c23 canaries are dormant, so an undeclared identifier in generated C is
otherwise invisible to the whole suite — which is why this survived to now.

## D3 — `try` and `spawn` are accepted inside `errdefer`

`checkDeferStatement` increments `names.cleanupDepth` (`compiler/checker/alloc.go:14-15`).
`checkErrdeferStatement` (`compiler/checker/errors.go:189`) does not. The guards
at `errors.go:117` and `concurrency.go:105` read only that counter.

Observed:

```
try inside errdefer     ACCEPTED
try inside defer        rejected: [Type Error] try is not permitted
                        inside defer or errdefer at 8:16
spawn inside errdefer   ACCEPTED
```

The existing diagnostic **already names `errdefer`**. The intended rule covers
both constructs; only the enforcement misses one.

`docs/reference.md` states `try` is invalid inside any cleanup action. A `try`
inside `errdefer` returns from a function that is already unwinding on Error,
producing corrupt control flow in the generated C.

### Fix

Increment and restore `cleanupDepth` in `checkErrdeferStatement`, matching
`checkDeferStatement`.

### Test

`try` and `spawn` inside `errdefer` are rejected with the same diagnostic the
`defer` forms already produce, including inside nested call arguments.

## D4 — `from_pointer` provenance is defeated by one copy

`fromRef` is set only when a declaration's initializer node is literally an
`AddressOfExpression` (`compiler/checker/declarations.go:391-393`).
`checkAssignment` never sets it, and `nodeTracesToRef`
(`compiler/checker/views_bridge.go:107-126`) stops at a variable read.

Observed:

```
from_pointer(ref a, 1)                       rejected  ✓
p: Ptr<Int32> = ref a;  from_pointer(p, 1)   rejected  ✓
p = ref a; q = p;       from_pointer(q, 1)   ACCEPTED  ✗
```

One level of indirection is tracked; a second defeats it. Assigning `ref a` into
an existing `mut` pointer is likewise untracked.

`docs/reference.md` states `from_pointer` "rejects pointers locally traceable to
`ref`". The result is a `View` over dead stack storage — the exact undefined
behavior View provenance exists to prevent.

### Fix

Propagate `fromRef` through variable-read initializers and through assignment.

### Test

The three shapes above, plus assignment into a `mut` pointer, plus a chain of
three copies. Heap-derived and parameter-derived pointers must remain accepted —
regression-test both so the fix does not over-reject.

## D5 — A private type escapes when an unrelated module exports its name

`nominalExported` (`compiler/checker/modules.go:513-518`) scans **all** entries
in `registry.modules` for `entry.exports[base]` rather than the defining
module's exports. That name loop short-circuits the correct pointer-identity
check immediately below it (`:517-525`).

Observed, with byte-identical `a.hex` in both runs:

```
a.hex alone                          rejected: exported function Wrap
                                     exposes private type Secret
a.hex + unrelated b.hex exporting
its own unrelated Secret             ACCEPTED
```

A private type reaches a public generated header because an unrelated module
happens to export a same-named type. This violates the exported-interface
closure rule in `docs/reference.md`.

### Fix

Delete the name loop, or scope it to `typ.Object.ModuleID`. The pointer-identity
loop below already implements the correct check.

### Test

Two modules defining same-named types, one exported and one private, in both
orders; plus the single-module control case.

## D6 — Generated C spells `NULL`

`docs/reference.md:1002` states: "Nil renders the C23 `nullptr` keyword and no
generated C spells `NULL` or `nullptr_t`."

Two sites violate it:

- `compiler/generator/render.go:392` — `"(" + rendered + " != NULL)"`
- `compiler/generator/render.go:1259` — `(hex_view_T){ NULL, 0 }`

The same file uses `nullptr` correctly in four other places.

`NULL` requires a declaring header. RFC 0062 made `<stddef.h>` demand-driven off
real `size_t` consumers, so a program with a nullable-pointer test and no
allocation, string, or I/O use emits `NULL` with nothing declaring it.
Determine whether that program is reachable and record the answer; the fix is
the same either way.

### Fix

Emit `nullptr` at both sites.

### Test

No generated artifact across the snippet corpus contains the token `NULL`.

## D7 — `Stats.ParseDuration` is never assigned

Declared at `compiler/compile.go:43`, summed at `:396`, rendered at
`workbench/main.go:129` as a "Parser" phase. No write exists anywhere; `:74-75`
folds lexing and parsing into `LexDuration`.

The workbench displays `0.00 ms` and `n/a LOC/sec` for that phase on every
compile.

### Fix

**Delete the field and the workbench row.** The value has been wrong since it was
introduced and nobody noticed, which is the measure of what it is worth.
Splitting the timer additionally requires deciding where import resolution
counts, since `LexDuration` currently folds lexing, parsing, and resolution
together — a decision with no demand behind it.

RFC 0074's `CompilationStats` determinism contract states which fields are
reproducible; removing a permanently-zero duration is the first step of that.

### Test

`CompilationStats` assertions covering every field, not only `SourceLines` and
`TokenCount != 0`. The absence of those assertions is why this survived.

## D8–D13, D28–D32 — Latent fail-open paths

Each is unreachable in the current tree. All were traced to ground and confirmed
latent, not live. They are in scope because each is the exact class `AGENTS.md`
forbids: a silent skip or a panic where a structured diagnostic is required.

| # | Site | Problem |
|---|---|---|
| D8 | `compiler/generator/walk.go:184-220` | `walkStatementExpressions` has **no `default:`**, while its own comment claims "An unknown statement shape is a generator error, never a silent skip." Its sibling `walkStatements` (`:515-517`) does fail closed. A new statement shape would silently skip try/spawn prologue hoisting — a miscompile, not a diagnostic. Same pattern at `errors.go:94` and `concurrency.go:522`. |
| D9 | `compiler/generator/emission.go:637-642` | `literalObjectName` returns `""` on a miss, producing `&,` in generated C. |
| D10 | `compiler/generator/concurrency.go:261-266` | `messageLiteral` substitutes the **source-filename literal** as an error message on a miss. |
| D11 | `compiler/generator/render.go:2059`, `:2074` | `signedMinimumMacro`/`signedMaximumMacro` panic outside `Int8/16/32/64`. Every caller guards with `IsSignedInteger`; a fifth signed type turns a diagnostic into a crash. |
| D12 | `compiler/checker/type_resolution.go:152` | `resolveUnionMemberUse`'s `*Diagnostic` is discarded while building a name list. A failed member yields an empty name, producing `"a union requires at least two distinct members;  \| Int32 has one"`. |
| D13 | `compiler/compile.go:83-85` | `lexer.Lex`'s error is dropped in the stats loop. Unreachable because the module is already parsed, but it is an ignored `error`. |
| D28 | `compiler/generator/generator.go:31-33`, `:42-53` | `GenerateChecked` silently `continue`s when a module named in `order` is absent from `programs`, and if no module matches `entrypointCanonical` the root pair and `hexal.h` are omitted **while the call still returns success**. Silent partial output is precisely what `AGENTS.md` forbids: "never return an empty result, emit a placeholder comment, or silently omit output." |
| D29 | `compiler/types/types.go:167-170` | `Diagnostic.Error()` substitutes `"Unknown Error"` when `Category` is empty. Any of the 258 hand-built `Diagnostic{}` literals that omits `Category` therefore reports a user error as a compiler inconsistency. Remove the default and make an empty category a construction error; RFC 0074 R9's single-factory work makes that enforceable. |
| D30 | `compiler/generator/packages/concurrency.c:12-18` | The `<threads.h>` include precedes the `#if defined(__STDC_NO_THREADS__)` `#error` guard, so a toolchain without `<threads.h>` fails on the include with a raw compiler error instead of the intended message. MinGW-w64 neither ships the header nor defines the macro. **Confirmed from source: the include does precede the guard.** Only the toolchain failure mode is unreproduced here, and reordering is correct regardless of which toolchain exhibits it. |
| D32 | `compiler/generator/validation.go:171` | **`validateStatements` returns instead of continuing on `print`.** Inside the loop over statements, the `PrintExpression` case does `return nil`, exiting the whole function — so **every statement after the first `print` in a block is never validated by generator preflight**. The comment ("print validates its arguments and produces no value") shows `continue` was intended. Not user-visible today because the checker rejects source-level errors first, but it silently disables defence-in-depth for the rest of the block. Reported by the Codex audit; **confirmed here by reading the code**. Fix: `return nil` → `continue`, then scan the same switch for the no-result deferred-call variant Codex also reports. |
| D31 | `compiler/tests/c23validation/` | 6 of 33 canary sources no longer compile against the current language — they use removed `.at()` and `try` in a non-Error function. Because the package has no runnable entry point, the drift is invisible. **Reported by the OpenCode audit; not independently verified.** Either repair them or delete them, but do not leave sources that assert nothing and no longer parse. |

D8, D9, and D10 are fixed by adding fail-closed defaults. D11 should return a
diagnostic rather than panic. D12 and D13 are one line each.

Note that D13 disappears entirely if RFC 0074's double-lex removal lands, since
the loop is deleted.

## D14–D16 and D18 — Normative-document contradictions

`docs/reference.md` is the sole normative contract; a contradiction inside it
has no correct reading.

| # | Where | Problem |
|---|---|---|
| D14 | `docs/reference.md:1002` | The rule forbids generated C from spelling `nullptr_t`. It appears 194 times in generated headers as part of union **identifier** encodings (`hex_union_7_int32_t9_nullptr_t`), which are not type spellings. The rule is unsatisfiable as written; narrow it to "as a type spelling". This is a documentation defect, not a compiler defect. |
| D15 | `docs/reference.md:689` vs `:705` | `:689` states `at` was removed; `:705` still describes "Indexing/at are checked" as live behavior. The compiler agrees with `:689` (`checker/arrays.go:208-209`, `lists.go:97-98` emit removal diagnostics). Delete the stale clause. |
| D16 | `docs/status.md:26`, `:52` | Two dead spec links. `specs/0071-…md` does not exist — 0071 was archived into `specs/archive/0071/`, the only spec in a subdirectory rather than flat. The 0069 link points at a `0069/` directory that does not exist. Fix the links and flatten the 0071 archive entry to match the other 60+. |
| ~~D17~~ | — | **Withdrawn — false finding.** Reported by an audit pass as "`0072`'s header `Status: Draft; implementation-ready` is self-contradictory and inconsistent with every other open spec." It is neither: `Draft; implementation-ready` is the established house convention, used by five archived specs and by 0073, 0075, 0076, 0077, and 0079. It means drafted and ready to implement, not drafted *or* ready. No action. |
| D18 | `docs/status.md:7` vs `:34-38` | `:7` states an item without a spec either gets one or gets deleted; the "Unowned" section at `:34` keeps exactly such an item. Resolve the item or amend the rule. |

## Required order

1. **D14–D16 and D18** — documentation only, no code. May land first.
2. **D1, D3, D5, D6, D20, D24, D33** — independent one-to-few-line fixes, each
   with its own test. Any order. D33 must land before or with RFC 0072, which
   inherits the same header requirement.
3. **D4** — needs the over-rejection regression tests written alongside.
4. **D21** — parser change; small, but re-run the full snippet catalog.
5. **D7** — delete the field and the workbench row, per its Fix section.
6. **D23** — self-contained runtime-template change with a boundary table.
7. **D22** — requires reusing normal call lowering rather than patching the
   whitelist.
8. **D2** — new discovery logic for handle payloads.
9. **D19 + D25 together** — largest and most invasive. They are one root cause
   from opposite directions and must be fixed by one canonical, module-qualified,
   per-compilation type key; fixing either alone risks trading one identity bug
   for the other. Land them **last** among the code defects so every other fix is
   already in place and the generated-C diff is attributable.
10. **D8–D13, D28–D31** — fold into whichever change touches each file, or land
    as one fail-closed sweep. D29 pairs naturally with RFC 0074 R9.

Regenerate the snippet SHA-256 manifest once, after all code defects land. D2,
D6, D19, D23, D24, D25, and D26 change generated output; the rest should not.

## Validation

- Each defect has a regression test that fails before the fix and passes after.
- `go test ./...`, `go vet ./...`, `go vet -tags c23 ./compiler/tests/c23validation`.
- The snippet catalog compiles and its manifest is regenerated deliberately.
- `docs/reference.md` is updated for D14 and D15 in the same change as the code
  it describes.
- No defect fix changes behavior beyond the defect: generated C for programs
  that do not exercise a defect is byte-identical.

## Non-goals

- Any refactor, deletion, or restructuring. RFC 0074 owns all of it.
- Adding a generated-C compilation gate. D2 makes the case for one; RFC 0074
  carries the proposal.
- Changing Hexal syntax or semantics. Every fix here makes the compiler match
  the contract `docs/reference.md` already states.

## Absorbed from RFC 0074

Three refactor items moved here because they are inseparable from a defect fix —
each edits the same function or loop as the defect beside it, so splitting them
across two specs would create an ordering dependency between the specs.

**A6 — remove the double lex** (was RFC 0074 R6). `compile.go:84` re-lexes every
reachable module solely to populate `Stats.TokenCount`, costing **4.53 MB, 4.3%
of compiler allocations**. It is the same loop as D13, which drops the resulting
lexer error. Return token counts from `reachableModules` and the loop disappears
with the defect.

**A7 — hoist the traversal closures** (was RFC 0074 R7).
`walkStatementExpressions` (`walk.go:144-180`) allocates two mutually recursive
Go closures **per statement**, across 21 discovery passes — **19.04 MB, 18.0%**.
Move them to package-level functions taking `visit` as a parameter, so nothing is
captured and nothing is allocated. This edits the same function as D8, which adds
its missing fail-closed `default:`. Add the guard first, then hoist.

**A10 — drop `error` from `walkProgram` and `programVisitor`** (was RFC 0074
R10). All 21 visitor callbacks return `nil` unconditionally and `walk.go:516`'s
default is unreachable, so the return value exists only to drive nine
`panic(err)` sites and one silent `_ = walkProgram(...)`. Removing it deletes all
ten and roughly 180 `if err != nil` lines. **Strictly after D8**: the fail-closed
arm must exist before the plumbing around it is simplified. The same sweep
removes the `error` returns from the discovery functions that existed only to
forward walk failures — `discoverGeneratedADTs`, `discoverHeapHelpers`,
`discoverGeneratedArrays`, `discoverGeneratedLists`, `discoverGeneratedDicts`,
`discoverGeneratedViews`, `discoverGeneratedStrings`, `discoverGeneratedUnions`,
`discoverEqualityTypes`, `discoverGeneratedConversions`,
`discoverGeneratedConcurrency` — and their call sites in `emission.go` and the
generator tests.

Do A6 with D13, and D8 → A7 → A10 in that order. RFC 0074 retains pointers so
the move is traceable.

## Readiness

**Implementation-ready — all defects.** Each has a reproduced failure or a cited
code site, a named fix, and a stated test. The one design question that blocked
D19 and D25 is resolved above.

D19 and D25 remain the largest item and stay last in the required order, because
they carry the `Environment` split. Nothing else in this RFC depends on them.

**One defect is reported but unverified here.** D31 (stale c23 canaries) was not
independently checked; confirm before fixing. D30 is source-confirmed — the
`<threads.h>` include does precede its guard — and only its toolchain failure
mode is unreproduced, which does not affect the fix: reordering is correct
whether or not a toolchain here can show it.

**Not fixed here, by decision: generated C that does not compile.** D2 and D26
both emit uncompilable C while every Go test passes. A toolchain-backed gate
would catch that class, and the project's standing rule is that no test invokes
an external compiler. That rule holds. When C-compiler validation is eventually
introduced, it will surface and fix this class as a batch — until then, each
instance is fixed individually as an ordinary defect, as D2 and D26 are here.
Do not propose a compile gate as part of this RFC.

## Excluded after verification

Recorded so they are not re-reported.

- **Nondeterministic exhaustiveness diagnostic.** Reported during the audit at
  `checker/adt.go:580-585` (arbitrary map key selected for the missing-variant
  message). **Already fixed** in the current tree: `:584` iterates
  `UnionMembers` in canonical declaration order with a `sort.Strings` fallback
  at `:596`. A 60-run probe produced one message. No action.
- **Generated-output nondeterminism generally.** 300 compiles of a four-module
  program with generics, collections, and handle types produced byte-identical
  artifacts every time, as did every catalog snippet over 40 runs. Every map
  iteration reaching output is sorted first. No action.
- **`gofmt` non-compliance.** `gofmt -l` reports three files; all three are
  working-tree CRLF artifacts under `core.autocrlf=true`, and the git index is
  uniformly LF. Normalizing produces a large diff carrying no change. RFC 0074
  records the opposing recommendation — an explicit `.gitattributes` LF policy —
  as a decision to make once rather than a defect.
- **Union canonicalization shares D19's omission.** Reported by the Codex audit
  and questioned by a later external review. **Did not reproduce.** Probed the
  D19 shape directly — two distinct module-owned objects both named `Point`,
  each unioned with `Bool`, both spelled in one module:

  ```
  hex_union_4_bool16_hex_t_m1_m_Point      /* M.Point | Bool */
  hex_union_4_bool16_hex_t_m1_s_Point      /* S.Point | Bool */
  ```

  Correctly discriminated. The mechanism is that a union's C name is composed
  from its members' `CName`s, which are already module-qualified — so the D19
  collapse is structurally impossible for unions. The same structural union
  spelled in two modules also unifies to one C type, as it should, so there is
  no D25-shaped over-discrimination either. D19's fix must not touch union
  identity.
- **`Error.new` always records `main.hex`.** Reported by the Codex audit
  (`compiler/checker/errors.go:17`). **Did not reproduce.** Probed twice: an
  `Error.new` in the entrypoint records `app.hex`, and an `Error.new` inside a
  non-entry module records `lib.hex`. Neither artifact contains `main.hex`.
  `main.hex` remains the correct synthetic name when no source key is supplied,
  per `docs/reference.md`. If a failing case exists it needs a reproducer before
  this is reopened.
- **Double-free, use-after-free, `h.free(ref stackLocal)`, and freeing a String
  literal all compile.** All verified accepted. **Excluded from this RFC because
  RFC 0079 owns them, not because they are correct.** An earlier revision of
  this bullet recorded them as by-design, quoting `docs/reference.md` on the
  absence of an ownership model; that reading has since been rejected. The
  reference names the mechanisms the language lacks, which is not a limit on
  what it diagnoses. `AGENTS.md` goal 18 and `docs/reference.md` have both been
  corrected, and RFC 0079 is implementation-ready with the first three cases
  scoped. Freeing a String literal stays undiagnosed — no local analysis decides
  it. The documentation contradiction this bullet previously reported is
  resolved; do not reopen it.
