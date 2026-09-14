# RFC 0185: AddressSanitizer Fiber Coverage

- Kind: Feature Specification (Rust-Style RFC)
- Status: Design not started; blocked on an ASan-capable execution toolchain
  and explicit fiber-switch instrumentation design
- Created: 2026-09-14
- Updated: 2026-09-14
- Scope: make AddressSanitizer trustworthy for generated programs, including
  user-space Task fiber switches
- Depends on: an ASan runtime that can compile, link, and execute on CI, plus
  the current Task context-switch implementation
- Does not add: language syntax, source semantics, or permission to claim ASan
  coverage before fiber annotations are correct

## Motivation

The installed Zig 0.16.0 Windows backend accepts `-fsanitize=address` but fails
to link a minimal program because its ASan runtime symbols are unavailable.
Custom fiber stacks additionally require ASan switch annotations; running ASan
over the current scheduler without them would produce untrustworthy results.

RFC 0183 therefore implements UBSan only. This RFC retains the ASan gap under
an explicit owner rather than weakening or silently skipping a release claim.

## Work required before design can settle

- Select a CI host and C toolchain that can compile, link, and execute ASan.
- Inventory every Task fiber switch and stack creation/destruction path.
- Design balanced `__sanitizer_start_switch_fiber` and
  `__sanitizer_finish_switch_fiber` calls for root, worker, parked, resumed,
  completed, and abandoned Tasks.
- Define leak-check ownership for process-lifetime runtime allocations and
  intentionally abandoned Tasks.
- Separate expected Hexal trap stderr from sanitizer diagnostics.
- Require a deliberately faulty canary proving that the configured ASan run
  detects invalid access.

## Readiness rule

Do not mark this RFC implementation-ready until the execution toolchain and
fiber-transition mapping are both concrete. Non-fiber ASan experiments may
inform the design but do not close this RFC.

## Reference synchronization

This work changes validation infrastructure, not the language contract. Do not
edit `docs/reference.md` from this RFC.

