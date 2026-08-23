# RFC 0113: Core End-of-Stream Values

- Kind: Feature Specification (Rust-Style RFC)
- Status: Discarded; low ROI while completion values remain compiler-owned
- Features: removal of builtin `EoS` and `eos`, replacement with a protected
  core unit value `End`
- Created: 2026-08-22
- Depends on: RFC 0029 (Error values), RFC 0034 (modules), RFC 0108
  (descriptor and memory streams), and `docs/reference.md`
- Coordinates with: Channels, byte streams, ADTs, unions, and RFC 0114
  (sum-type surface)

## Summary

Remove `EoS` as a protected compiler type and remove the `eos` literal. End of
input is a protected zero-payload core value named `End`.

Stream-like operations use unions containing `End`:

```text
read(...) -> Size | End | Error
receive() -> T | End
```

`End` is a protected core nominal unit type. It uses the ordinary union,
narrowing, comparison, and C representation machinery; it has no compiler-only
narrowing path.

## Semantics

- `End` is available without an import and cannot be redeclared or shadowed.
  No global protected `EoS` name exists.
- A `T | End` union narrows using the ordinary union/member rules.
- A stream or channel returns `End` only when its documented completion state
  is reached. Completion is not an Error.
- `End` is valid as an ordinary List, Dict, or allocator element.
- `Channel<End>` is rejected because `receive()` would have the
  indistinguishable result `End | End`. Other channel element types retain
  their existing restrictions.
- Whether source code may construct an `End` value is specified by the
  construction decision below; compiler-owned channel and stream operations
  can always produce it.
- `Seek.End` remains a separate qualified ADT variant describing a seek
  position. It is not the completion value.

## API migration

- `Channel<T>.receive() -> T | EoS` becomes `T | End`.
- `IO.read(...) -> Size | EoS | Error` becomes `Size | End | Error`.
- `Bytes.read(...) -> Size | EoS | Error` follows the same rule.
- Existing `is EoS` checks become `is End` checks; no import is required.
- `try` and `errdefer` continue to recognize only `Error` as propagation;
  `End` remains an ordinary success alternative.
- Generated C represents `End` using the ordinary ADT/union tag machinery. No
  `hex_eos` typedef, special tag, or `eos` constructor is emitted.

## Non-goals

- Changing whether a particular stream reports a short read, completion, or
  Error.
- Making all completion states across unrelated APIs one nominal type without
  an API-level decision.
- Replacing `Seek.End`.
- Adding an option type or exception mechanism.

## Validation

This section is exhaustive. RFC 0113 is complete only when every item below
passes:

- `EoS` and `eos` are rejected as source names and literals.
- `End` is available without an import and can be used in ordinary union
  narrowing.
- `T | End` unions narrow with the ordinary member rules.
- `List<End>` and `Dict<String, End>` are accepted, while `Channel<End>` is
  rejected because its completion alternative is ambiguous.
- Channel receive returns `T | End` and reports `End` only after close and
  drain.
- IO and Bytes reads return `Size | End | Error` with unchanged transfer and
  error behavior.
- `try` propagates Error and does not treat End as an error.
- `Seek.End` remains distinct from stream completion.
- Generated C contains no EoS-specific typedef, literal, or tag path.
- Repeated compilations produce identical artifacts.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, remove `EoS`, `eos`, and their special grammar,
type, comparison, eligibility, and C-output rules from `docs/reference.md`.
Record protected core `End`, the collection/channel restrictions, and the final
source-construction rule. Update the Channel and stream contracts to use `End`
without an import.

## Open construction decision

The remaining decision is whether user code can construct an `End` value. The
compiler-owned Channel, IO, and Bytes operations produce it internally either
way. No separate unit-variant spelling is needed unless source construction is
allowed.

- Recommended: do not allow source construction initially. Only core
  producers create `End`; user code can receive, narrow, store, pass, and
  return an existing `End` value.
- If source construction is required for user-defined producers, make `End`
  itself the protected value spelling:

  ```hexal
  return End
  ```

  Do not introduce an ADT-style spelling such as `End.End`; it adds syntax
  without adding semantics.
