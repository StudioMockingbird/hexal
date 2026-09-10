# RFC 0158: Consuming Method Receivers and a Debug Tracking Allocator

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion (proposal); not scheduled. Promote to
  Implementation-ready only after RFC 0149 (method receivers) and RFC 0110
  (automatic drop and early-cleanup disarming) land
- Created: 2026-09-10
- Updated: 2026-09-10
- Depends on: RFC 0149 (`method T` / `method mut T` receiver model), RFC 0110
  (implicit moves, automatic drop, explicit-cleanup disarming)
- Coordinates with: RFC 0039 (foreign resources whose close may fail stay
  explicit there, not here)
- Does not update `docs/reference.md`: synchronize only after implementation
  is approved and behavior stabilizes

## Summary

Automatic drop makes memory leaks impossible, but user-defined resources
(file descriptors, handles, locks) get no help: no user drop hooks exist, no
method can consume its receiver, and nothing rejects use-after-close or
double-close. This proposal closes that gap with two complementary,
individually shippable mechanisms:

1. **Consuming method receivers**: a third receiver form alongside
   `method T.name` and `method mut T.name`, taking `self` by value. Calling
   it moves the receiver, disarms automatic drop exactly like an explicit
   early cleanup, and makes every later use a use-after-move error.
2. **A debug tracking allocator**: a test-only backend that records live
   allocations and reports leaks at exit — Odin's `Tracking_Allocator`
   equivalent. Pure runtime facility, zero language surface, turning
   "remember to close" from convention into CI signal.

## Problem

- A `Socket.close()`, `FileHandle.release()`, or arena `reset()` wrapper
  cannot express consumption: RFC 0149 specifies only Ref receivers, so a
  close method either borrows (leaving a closed-but-usable value behind,
  inviting double-close) or is a free function taking the value by parameter
  (losing method-call ergonomics and discoverability).
- After a manual close, automatic drop still runs at scope exit. For
  memory-only members that is harmless; for a member the close already
  released through a side channel, drop must be disarmed — and only
  compiler-owned cleanups currently disarm it (RFC 0110).
- Convention-only cleanup ("remember to call close, and don't call it
  twice") is exactly the C failure mode Hexal's memory story just escaped.
  Without either compile-time or test-time checking, resource discipline
  regresses to documentation.

## Goals

- Give user resources C++-style consuming methods with Rust-style move
  checking, reusing (not extending) the affine flow machinery: consume is a
  move, use-after-consume is use-after-move, no new analysis is introduced.
- Keep the receiver spelling parallel to the existing two forms so the three
  read as one rule: borrow-read, borrow-write, take.
- Detect leaked resources in tests with no source changes: run the same
  program under the tracking backend and get a leak report.
- Ship either half alone: consuming receivers need no runtime work, and the
  tracking allocator needs no language work.

## Non-goals

- User-defined drop hooks or destructors. Consuming methods are explicit
  calls, never implicit; drop plans remain compiler-derived.
- Fallible close: a close that can return Error stays an ordinary explicit
  method under RFC 0039's future failure policy, not a consuming receiver
  (a consumed-then-failed value has nowhere coherent to go).
- Production leak detection, thread-safe tracking, or allocation profiling:
  the tracking backend is a test and workbench facility.
- Reviving cleanup obligations: forgetting `close()` remains compilable;
  part 2 catches it in tests instead of at compile time.

## Proposed design

### Part 1: consuming receivers

```hexal
method owned Socket.close() do
    raw_close(self.fd)
end

sock := open_socket(h)
sock.close()
sock.close() // Type Error: sock was moved
```

- `method owned T.name` declares a by-value receiver. The call implicitly
  moves the receiver place into the method; the source binding becomes
  unavailable under the ordinary moved-source rules.
- The move disarms automatic drop for that value exactly as RFC 0110's
  explicit early cleanup does: the method body owns `self` and its exit drops
  whatever remains unmoved, so a close that releases side-channel state and
  leaves memory members behind cleans up fully with no double-release.
- Fixed versus writable roots follow the existing place rules: consuming a
  fixed binding's value is allowed (consumption is not reassignment), while
  the place must still be a movable affine value.
- Compiler-owned members keep precedence over pointee members per RFC 0149;
  a user `owned` method on a nominal type never collides with generated drop
  helpers, which remain internal and unspeakable in source.
- Passed-through `self` (returned or moved onward from the body) follows
  ordinary move rules; no special receiver-return form is introduced.

### Part 2: debug tracking allocator

- A backend selected at build time (workbench flag or `Project` setting, see
  open question 4) that records every live allocation with its source site
  and size, removes the record on deallocation, and emits the outstanding set
  to stderr at program exit (or on explicit report call).
- Tracking covers all three families (Heap, Stash regions, Pool slots) and
  both explicit and automatic-drop releases, so a forgotten `close()` whose
  backing allocation survives shows up as a leak with its allocation site.
- Intended consumers are tagged C23 tests and workbench runs. Ordinary pure-Go
  tests never execute generated C and are unaffected; no test currently
  passing changes behavior when tracking is off, which is the default.
- Overhead when enabled is one record per live allocation; when disabled the
  backend compiles to the existing direct calls with no branches on the
  allocation path.

## Diagnostics

- Use, borrow, move, or second consume of a consumed receiver reports the
  existing use-after-move diagnostic naming the source binding.
- Consuming calls through `Ref<T>`-borrowed places (e.g. consuming a field
  through a borrowed method receiver) are rejected as moves out of borrowed
  state, not as receiver mismatches.
- A consuming method declared on a copyable T is rejected: consumption of a
  value the language copies freely is almost certainly a signature error.
- Tracking output names Hexal source sites via the existing `#line` mapping,
  never generated C identifiers.

## Acceptance sketch

Non-exhaustive: the implementing RFC (or RFCs, if the halves split)
promotes this to an exhaustive Validation section.

- `method owned` consumes exactly once; every later source use fails;
  generated C shows one move plus the body's statements and no extra drop.
- Body-exit drop of unmoved remainder releases memory members exactly once;
  side-channel state closed in the body is never re-closed by generated code.
- Copyable receiver consumption fails at declaration time.
- Under the tracking backend, a program leaking one allocation reports
  exactly that allocation with its source site; a clean program reports
  nothing and exits zero.
- Tracking disabled changes no generated allocation call and no manifest
  hash.

## Open questions

1. Spelling: `method owned T.close()` (proposed, parallels `method mut`) versus
   `method consume T.close()` (verb form, reads as action). `owned` is
   recommended for parallel structure.
2. Whether a consuming method may be declared on `Box<T>` itself (taking the
   pointer by value) or only on nominal types, and how that interacts with
   `Box.free()` precedence.
3. Whether consuming receivers should be allowed in `extern c` member
   position or are Hexal-only until RFC 0039 says otherwise.
4. Tracking selection: `Project` build-time setting versus a workbench-only
   flag. A setting composes with future sanitizers; a flag keeps `Project`
   minimal.
5. Whether the tracking report should trap (nonzero exit) or print-and-exit-zero;
   trapping composes with fail-closed CI, printing composes with log-scraping.
