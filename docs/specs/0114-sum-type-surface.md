# RFC 0114: Sum-Type Surface Simplification

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Features: one primary user-facing sum model, nullable/result sugar, and
  restricted anonymous unions
- Created: 2026-08-22
- Depends on: RFC 0019 (generics), RFC 0029 (Error values), RFC 0034
  (modules), RFC 0099 (program-wide discriminants), RFC 0113 (End), and
  `docs/reference.md`
- Coordinates with: C interop RFC 0039, affine ownership RFC 0110, and the
  language-surface audit

## Problem

Hexal currently exposes several overlapping sum mechanisms:

- structural unions such as `T | U`;
- nominal ADTs;
- nullable unions using `Nil`;
- compiler-owned `Error` unions; and
- compiler-owned `EoS` completion values.

Each mechanism is defensible in isolation, but the combined surface creates
multiple rules for construction, narrowing, equality, representation, tags,
and diagnostics. The language needs one obvious primary way to define a closed
sum while retaining concise spelling for the two common systems cases:
optional values and fallible results.

## Benefits

One primary sum model removes duplicate grammar, checker dispatch, narrowing
rules, tag-registration paths, generated representations, diagnostics, and
test matrices. It also gives affine ownership, Error propagation, End values,
and C interop one place to define their sum behavior. Generic `Option` and
`Result` forms preserve concise everyday code, while named ADTs make complex
states discoverable and reusable. The cost is a small amount of declaration
ceremony for uncommon anonymous alternatives; that cost is intentional and
prevents every call site from inventing a new structural type identity.

## Proposed direction

Nominal ADTs become the primary user-facing sum declaration:

```text
type Result<T, E> =
    | Ok { value: T }
    | Err { error: E }
end
```

Anonymous unions remain surface sugar only for:

- nullable values, written by the settled nullable syntax;
- fallible results containing the protected `Error` type; and
- explicitly declared foreign ABI representations.

General user-defined `T | U | V` unions are replaced by a named ADT or a
standard generic sum type. The compiler may continue to use structural union
interning internally for normalized nullable/result and foreign representations,
but that implementation identity is not a source-language declaration form.

## Semantic rules

- Every user-defined closed sum has a nominal owner and a fixed variant set.
- Variants may be unit or record-carrying.
- Generic ADTs are monomorphized and retain their existing specialization rules.
- `match` over an ADT is exhaustive over qualified variants.
- Nullable sugar has one canonical representation and uses the null niche only
  where the target representation permits it.
- Fallible result sugar has one canonical Error member and remains compatible
  with `try` and `errdefer`.
- Foreign unions are never inferred as native ADTs. They retain foreign
  layout identity and are unsafe unless the target profile proves a checked
  representation.
- `Nil`, `End`, and `Error` are ordinary members of their owning sum/API
  contract, not a second pattern language.
- A sum value has one active variant and no implicit conversion to a surviving
  member. Narrowing is required before member access.

## Migration direction

- Replace arbitrary source unions with named ADTs or standard `Option<T>` and
  `Result<T, E>` forms.
- Replace `T | Error` with the selected fallible-result sugar or a named Result
  type without changing `try` propagation semantics.
- Replace `T | EoS` with `T | End` under RFC 0113 before removing EoS.
- Keep generated tagged unions for ADTs and normalized compiler representations.
- Keep C union support inside RFC 0039's unsafe foreign boundary.

This RFC does not choose the final spelling of nullable and result sugar. It
does choose that arbitrary anonymous source unions must not remain a second
general sum-declaration mechanism.

## Non-goals

- Removing tagged unions from generated C.
- Removing generics or pattern matching.
- Making foreign C unions safe by default.
- Adding inheritance, open sums, or runtime reflection.
- Changing affine ownership or View lifetime rules.

## Validation

This section is exhaustive. RFC 0114 is complete only when every item below
passes:

- A user-defined ADT declares a nominal closed variant set and supports unit
  and record variants.
- ADT construction, narrowing, matching, equality, and generated tags use one
  semantic path.
- Generic ADTs specialize deterministically without duplicate definitions.
- Nullable and fallible-result sugar preserve their current checked behavior
  and representation guarantees.
- Arbitrary source unions outside the retained sugar and foreign forms produce
  a structured diagnostic with migration guidance.
- `End` participates through its ordinary library-owned unit value rather than
  a compiler-only EoS path.
- Foreign C unions remain available only through the explicit C interop trust
  boundary and target-profile evidence.
- Generated C emits one canonical tagged representation per reachable ADT or
  normalized compiler-owned sum.
- Repeated compilations produce identical tags, names, and artifacts.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, rewrite the union and ADT sections of
`docs/reference.md` together. Remove duplicate structural-union surface rules,
retain only the settled nullable/result sugar, and update C23 representation
and diagnostic contracts in the same change.
