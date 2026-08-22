# RFC 0113: Library End-of-Stream Values

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Features: removal of builtin `EoS` and `eos`, replacement with a library
  unit value `End`
- Created: 2026-08-22
- Depends on: RFC 0029 (Error values), RFC 0034 (modules), RFC 0108
  (descriptor and memory streams), and `docs/reference.md`
- Coordinates with: Channels, byte streams, ADTs, unions, and RFC 0114
  (sum-type surface)

## Summary

Remove `EoS` as a protected compiler type and remove the `eos` literal. End of
input is an ordinary zero-payload library value named `End`.

Stream-like operations use unions containing `End`:

```text
read(...) -> Size | End | Error
receive() -> T | End
```

`End` is defined using the ordinary unit-variant machinery. It has no special
literal, comparison rule, C representation, or compiler-only narrowing path.

## Semantics

- `End` is a nominal unit ADT or equivalent standard-library unit value.
- A source imports the module that owns `End`; no global protected `EoS` name
  exists.
- A `T | End` union narrows using the ordinary union/member rules.
- A stream or channel returns `End` only when its documented completion state
  is reached. Completion is not an Error.
- `End` is not a valid collection element, allocator element, or channel
  payload when that would make completion recursive or ambiguous; the owning
  API specifies these restrictions.
- `End` has no standalone literal syntax. Construction uses the ordinary unit
  variant spelling when a source must construct it.
- `Seek.End` remains a separate qualified ADT variant describing a seek
  position. It is not the completion value.

## API migration

- `Channel<T>.receive() -> T | EoS` becomes `T | End`.
- `IO.read(...) -> Size | EoS | Error` becomes `Size | End | Error`.
- `Bytes.read(...) -> Size | EoS | Error` follows the same rule.
- Existing `is EoS` checks become `is End` checks after importing the owning
  library type.
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
- The library `End` value can be imported and used as an ordinary unit variant.
- `T | End` unions narrow with the ordinary member rules.
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
Update the Channel and stream contracts to import and use `End`.
