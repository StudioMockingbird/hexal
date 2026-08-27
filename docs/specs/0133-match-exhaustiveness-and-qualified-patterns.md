# RFC 0133: Match Exhaustiveness and Qualified Patterns

- Kind: Language Semantics
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Updated: 2026-08-27
- Restores: the current `docs/reference.md` match contract; no new pattern
  family or match mode
- Coordinates with: RFC 0103 findings F2, F29, and F31
- Does not own: non-Bool value patterns, pattern alternation, omitted ADT
  owners, guards, destructuring patterns, or open/infinite-domain
  exhaustiveness

## Summary

Make the existing closed-domain match checker complete and identity-safe.

The current implementation has three independent defects:

1. A dotted arm such as `M.Point` is parsed as an ADT variant before the
   checker knows whether `M` is a module alias. Imported nominal union members
   therefore cannot be matched.
2. Union coverage is keyed by `Type.Name`. Distinct members with the same
   short name, such as `M.Point | S.Point`, collapse into one case even though
   the type system correctly keeps their canonical identities distinct.
3. Exact non-union type patterns have no coverage entry, and `else` does not
   verify that a case remains. Duplicate exact-type arms and a final `else`
   after complete coverage are accepted as reachable.

This RFC changes no source spelling. It makes the parser preserve ambiguous
dotted pattern syntax until the checker has the scrutinee and module context,
then checks every arm against one ordered coverage model.

## Current normative contract

The reference already requires:

- value mode to match `true` and `false`;
- type mode to match exact complete types, union members, `Nil`, and ADT
  variants;
- an optional final `else` to cover all remaining values;
- exhaustive matches; and
- rejection of duplicate patterns and patterns unable to match any remaining
  value.

The work below restores those rules. It does not add semantics that the
reference currently lacks.

## Verified evidence

The following in-memory compilations were run against the 2026-08-27 tree.

### Imported nominal pattern is misclassified

```hexal
module M = import "./m"
module S = import "./s"

u: M.Point | S.Point := M.make()
result: Int32 := match u is
| M.Point then 1
| S.Point then 2
end
```

Current result:

```text
exit=1
[Type Error] unknown qualified variant M.Point at app.hex:5:5
```

`compiler/parser/expressions.go:matchPattern` converts every simple
`identifier.identifier` arm into `VariantPattern`. It never gives the type
resolver an opportunity to treat the same tokens as
`QualifiedTypeExpression`.

### Imported ADT pattern does not resolve through its module alias

Given `m.hex`:

```hexal
export type Shape as
| Circle
| Square
end
```

this valid match fails:

```hexal
module M = import "./m"

x: M.Shape := M.Circle
y: Int32 := match x is
| M.Circle then 1
| M.Square then 2
end
```

Current result:

```text
exit=1
[Type Error] unknown qualified variant M.Circle at app.hex:4:5
```

Variant construction already resolves an import-alias owner through the
module registry. Match checking bypasses that path and queries only the local
type environment.

### Duplicate exact type is accepted

```hexal
x: Int32 := 1
y: Int32 := match x is
| Int32 then 1
| Int32 then 2
end
```

Current result: `exit=0`.

The checker creates remaining cases for unions, ADTs, and value-mode Bool,
but not for an exact non-union type.

### Unreachable `else` is accepted

Both programs currently compile:

```hexal
x: Int32 := 1
y: Int32 := match x is
| Int32 then 1
| else then 2
end
```

```hexal
x: Bool := true
y: Int32 := match x
| true then 1
| false then 2
| else then 3
end
```

The final `else` cannot receive a value in either program and violates the
reference's unreachable-pattern rule.

### Short-name coverage is not type identity

`checkMatchExpression` currently records union cases as:

```text
remaining[member.Name] = true
```

The type system explicitly defines `CanonicalKey`, not `Name`, as recursive,
module-qualified identity. Distinct legal union members may share `Name`:

```hexal
M.Point | S.Point
Ptr<M.Point> | Ptr<S.Point>
M.Shape | S.Shape
```

The parser defect currently prevents the direct nominal probe from reaching
this map. Once dotted patterns resolve, retaining the map would turn one
parser fix into silent false exhaustiveness. The coverage representation and
qualified-pattern repair therefore land together.

## Semantics

### 1. Closed coverage domains

The checker constructs one ordered coverage domain before checking arms:

| Match form | Cases |
|---|---|
| value-mode `Bool` | `false`, `true` |
| type-mode union | each canonical union member |
| type-mode ADT | each declared variant |
| type-mode complete non-union, non-ADT type | the exact scrutinee type |
| other value-mode type | open domain; only `else` is currently a valid arm |

- Union cases use canonical type identity, never `Type.Name`, `CName`, source
  coordinates, or a rendered spelling.
- ADT cases use the scrutinee ADT identity plus the declared variant index.
- Bool cases use their two value identities.
- The order is Bool order above, canonical union-member order, ADT declaration
  order, or the sole exact type.
- The same ordered table owns membership, remaining coverage, lowering tags,
  and the first-missing diagnostic. Parallel maps or a second name-based
  coverage model are forbidden.

### 2. Arm checking

For every explicit arm:

1. Resolve the pattern against the scrutinee and current module context.
2. Reject it with `match pattern does not belong to the scrutinee type` when
   its canonical case is outside the complete domain.
3. Reject it with `duplicate or unreachable match pattern` when its canonical
   case belongs to the domain but was already covered.
4. Otherwise mark exactly that case covered and apply the existing arm-local
   narrowing.

For `else`:

- it remains legal only as the final arm;
- it is reachable when at least one closed case remains, or when the
  scrutinee has the open value-mode domain;
- it marks every remaining case covered;
- it reports `duplicate or unreachable match pattern` when a closed domain is
  already fully covered.

After the final arm, the first uncovered closed case reports:

```text
match is not exhaustive; missing <case>
```

An open value-mode domain is exhaustive only when it contains `else`.

### 3. Dotted patterns

The parser must not decide that `A.B` is a variant merely from its token
shape. It records a neutral dotted match pattern and leaves classification to
the checker.

The checker resolves the neutral form by scrutinee domain:

- for an ADT scrutinee, `A.B` denotes variant `B` of local/generic ADT owner
  `A`, or exported variant `B` reached through import alias `A`;
- for a union or exact non-ADT scrutinee, `A.B` denotes the import-qualified
  type `B` from module alias `A`;
- a resolved type pattern must equal the exact case or one canonical union
  member;
- a resolved variant must belong to the exact scrutinee ADT;
- visibility and unknown-alias diagnostics remain owned by existing module
  resolution.

Explicit generic ADT variant patterns retain their current spelling and
semantics. This RFC does not add imported generic-type syntax.

### 4. Missing-case names

- A unique builtin or local case retains its current short diagnostic name.
- An imported ADT variant uses its module alias and variant name, such as
  `M.Square`.
- An imported nominal case uses the current module's import alias, such as
  `S.Point`.
- A constructed case qualifies its nominal leaves, such as
  `Ptr<S.Point>`.
- When more than one alias reaches the same module, diagnostic rendering uses
  the lexicographically first alias.
- Missing-case rendering is diagnostic-only. It never participates in type
  identity or coverage.
- `CanonicalKey` and generated C names are never exposed to the user.

### 5. Narrowing and lowering

- A qualified union-member arm narrows the named scrutinee to that exact
  canonical member, including when another member has the same short name.
- An imported ADT arm narrows to the selected variant and exposes only that
  variant's payload.
- Arm order, scrutinee-once evaluation, result-type agreement, and generated
  switch/tag representation do not change.
- This is checker/parser work. The generator consumes the corrected checked
  arm tags and requires no independent exhaustiveness logic.

## Diagnostics

Preserve the existing exact messages:

```text
else must be the final match arm
match pattern does not belong to the scrutinee type
duplicate or unreachable match pattern
match is not exhaustive; missing <case>
```

Also preserve existing mode errors, unknown type/module/variant errors, and
arm-result disagreement diagnostics. Earliest ownership is parser for malformed
arm syntax, module/type resolution for an unknown qualified name, and match
checking for membership, reachability, and exhaustiveness.

## Implementation map

| Area | Required change |
|---|---|
| `compiler/parser/ast.go` | replace syntactically preclassified simple dotted variant patterns with one neutral dotted pattern record |
| `compiler/parser/expressions.go` | parse the ambiguous dotted arm without assigning type-versus-variant meaning |
| `compiler/parser/adt_test.go` | assert neutral parsing and preserve generic-variant parsing |
| `compiler/checker/adt.go` | build and consume the ordered canonical coverage table; resolve dotted patterns through scrutinee context and module registry |
| `compiler/checker/modules.go` | reuse existing exported-type and exported-ADT-variant lookup; add only a narrow reverse-alias helper if diagnostic rendering needs it |
| `compiler/checker/adt_test.go` | cover exact-type reachability, duplicate arms, `else`, deterministic missing cases, and local/generic ADTs |
| `compiler/tests/integration/adt_test.go` | cover imported ADTs and imported same-named union members end to end |
| `docs/reference.md` | make dotted-pattern semantic disambiguation and closed-domain reachability explicit after implementation stabilizes |
| `docs/status.md` | remove this bug row when the RFC closes |

## Required sweep

- Remove every match-coverage lookup keyed only by `Type.Name`.
- Remove or rename AST/comments that claim a simple dotted pattern is always
  an ADT variant.
- Reuse module alias, exported type, and exported ADT lookup; do not add a
  second import-resolution path.
- Keep generic ADT specialization and explicit generic-owner tests green.
- Keep union canonical ordering and same-named nominal identity tests green.
- Do not change the generator's own switch exhaustiveness or strict-C warning
  work owned by RFC 0131.
- Do not absorb RFC 0103's optional pattern-surface proposals.

## Detailed implementation plan

### Phase 0: freeze evidence

1. Record the ordinary Go test/vet and snippet-manifest baseline.
2. Re-run the five probes in Verified evidence and retain their exact results.
3. Record every match test in parser, checker, and integration packages.
4. Inventory every `remaining` lookup, arm-tag append, and narrowing call in
   `checkMatchExpression`.

### Phase 1: preserve dotted syntax

1. Add the neutral dotted-pattern AST node.
2. Make `matchPattern` emit it for simple dotted spellings.
3. Preserve `Result<T>.Ok` and other explicit generic variant forms.
4. Add parser tests proving the parser no longer assigns semantic ownership
   to `M.Point`.

### Phase 2: canonical coverage table

1. Introduce the small ordered match-case record.
2. Populate it for Bool, union, ADT, exact type, and open value mode.
3. Replace short-name map membership with canonical equality or variant
   identity.
4. Derive lowering tags and first-missing order from the same records.
5. Add focused checker tests before changing module-qualified resolution.

### Phase 3: qualified resolution and narrowing

1. Resolve a dotted ADT arm through local/generic owner resolution or the
   existing imported-variant registry path.
2. Resolve a dotted union/exact-type arm through the existing qualified-type
   resolver.
3. Consume the resolved canonical case and pass it to existing narrowing.
4. Add end-to-end tests whose two same-named object arms access different
   payload members, proving the narrowing identities do not collapse.

### Phase 4: reachability and diagnostics

1. Reject an explicit case after it has been covered.
2. Reject final `else` after complete closed coverage.
3. Keep `else` reachable for an open value-mode domain.
4. Add qualified missing-case rendering without exposing canonical or C names.
5. Verify repeated compiles report the same first missing case and message.

### Phase 5: documentation and validation

1. Run every Validation item below.
2. Update the match grammar/semantics in `docs/reference.md`; do not add
   examples or tutorial prose.
3. Remove this bug row from `docs/status.md` and close/archive the RFC only
   after code, tests, and reference agree.
4. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
5. Rebuild and restart the workbench for handoff.

## Validation

This section is exhaustive.

### Existing behavior retained

- Value-mode Bool with exactly `true` and `false` compiles.
- A final `else` covers a partially covered Bool, union, or ADT.
- Type-mode scalar with its one exact type arm compiles.
- Local ADT variants narrow payload fields only inside their arm.
- Generic ADT variants, with inferred or explicit owner arguments, compile
  and remain exhaustive.
- A scrutinee is evaluated exactly once.
- Arm results retain existing expected-type and agreement rules.
- `else` before the final position keeps its existing diagnostic.
- A foreign type or variant pattern keeps the membership diagnostic.

### Reproduced defects closed

- Two identical exact scalar arms reject the second as
  `duplicate or unreachable match pattern`.
- An exact scalar arm followed by `else` rejects `else` as unreachable.
- `true`, `false`, then `else` rejects `else` as unreachable.
- Every explicit union member followed by `else` rejects `else` as
  unreachable.
- Every ADT variant followed by `else` rejects `else` as unreachable.
- An imported ADT matched with all `M.Variant` arms compiles and narrows each
  payload correctly.
- An imported ADT missing one variant reports that qualified variant.
- `M.Point | S.Point` with both qualified arms compiles; each arm narrows to
  its own nominal type.
- The same union with only the `M.Point` arm rejects as non-exhaustive and
  reports `S.Point`.
- Reversing written union-member order does not change identity, coverage, or
  the deterministic first-missing rule.
- The same complete and missing-arm assertions cover
  `Ptr<M.Point> | Ptr<S.Point>` and `M.Shape | S.Shape`.

### Architecture and regression gates

- No coverage or duplicate test uses `Type.Name` as identity.
- Parser tests prove simple dotted syntax remains neutral until checking.
- Module resolution has one authoritative import/export path.
- No existing snippet-manifest hash changes; this repair admits new programs
  and rejects previously invalid ones but does not change output for an
  already-valid equivalent program.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

Implementation must update only the existing grammar and match-contract
locations in `docs/reference.md`:

- describe the neutral dotted match-pattern syntax without claiming it is
  always a variant;
- state semantic disambiguation by scrutinee domain;
- state canonical identity for union coverage;
- state exact non-union type coverage; and
- state that `else` is invalid when no value remains.

Do not add examples, implementation history, diagnostics internals, or a
second match section. No reference edit is part of drafting this RFC.

## Accepted costs

- One small ordered coverage record replaces a shorter name-keyed map.
- Qualified missing-case diagnostics need deterministic alias-aware rendering.
- The parser AST carries one syntactically neutral form to preserve the
  parser/checker responsibility boundary.

These costs are bounded and remove silent false exhaustiveness.

## Readiness

Implementation-ready. The language contract is already settled by the
reference; the required work is a verified conformance repair.
