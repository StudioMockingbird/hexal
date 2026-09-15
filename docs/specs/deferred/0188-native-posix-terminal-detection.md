# RFC 0188: Native POSIX Terminal Detection

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; reactivate after RFC 0183 Track 6 qualifies a POSIX target
  runtime gate
- Created: 2026-09-15
- Updated: 2026-09-15
- Scope: replace libuv-only POSIX terminal classification with native `isatty`
- Depends on: the implemented Terminal surface from closed RFC 0174 and a
  future qualified POSIX target profile
- Does not add: syntax, public APIs, raw mode, resize events, or a new terminal
  representation

## Decision

Implement `Terminal.is_attached(stream)` on POSIX with native `isatty` after
the existing `fcntl(F_GETFD)` validity check. `Terminal.size(stream)` uses the
same classification and retains `ioctl(TIOCGWINSZ)` for dimensions.

Do not use `uv_guess_handle` for this Boolean query. A program selecting only
Terminal detection or dimensions must not select libuv, mimalloc, native
bootstrap, the event bridge, or the scheduler.

This changes only the POSIX generated backend and its dependency set. Windows
retains `GetFileType`, `GetConsoleMode`, and `GetConsoleScreenBufferInfo`.

## Error contract

- An invalid descriptor remains Error through the existing native mapper.
- A valid descriptor for which `isatty` returns one is attached.
- `isatty` returning zero with `errno == ENOTTY` is successful false.
- Any other nonzero errno is `terminal detection failed` through the existing
  native mapper.
- Retry only an interrupted `fcntl` or `isatty`; do not retry the dimension
  query.

## Readiness blocker

The current compiler registry qualifies only Windows x64. Do not change and
close an unexecuted POSIX runtime branch. Implementation begins when one POSIX
profile can compile, link, and execute the complete Terminal fixtures through
the external C23 gate.

## Reactivation condition

Reactivate immediately after the first POSIX profile passes RFC 0183 Track 6's
complete compile, link, and runtime qualification gate.

## Detailed implementation plan

1. Record the POSIX Terminal component and dependency baseline from a qualified
   profile.
2. Replace both `uv_guess_handle` calls in `terminal.c` with one shared native
   classification helper using `isatty` and the exact error rules above.
3. Remove `<uv.h>`, libuv selection, and native-bootstrap selection caused only
   by POSIX Terminal queries.
4. Keep Windows output and query code byte-identical.
5. Add generated-text and tagged POSIX runtime tests for attached terminal,
   pipe/file false, invalid descriptor, interrupted query, and size.
6. Run ordinary tests, the qualified POSIX external gate, and the snippet
   manifest; inspect dependency movement.
7. Update the Terminal backend contract in `docs/reference.md` only after
   behavior stabilizes and only with explicit user approval.

## Validation

This list is exhaustive:

- POSIX attached terminal returns true;
- POSIX pipe and file return false without Error;
- invalid descriptor and non-ENOTTY failure use the existing source-located
  Error mapping;
- interrupted validity/classification calls retry and interrupted dimension
  query does not;
- dimensions still use visible positive `ioctl(TIOCGWINSZ)` results;
- Terminal-only POSIX programs emit no libuv, mimalloc, event, scheduler, or
  native-bootstrap dependency;
- Windows Terminal and Unicode-print artifacts remain byte-identical; and
- exact generated-C compilation and execution on at least one qualified POSIX
  profile.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation changes
only the POSIX Terminal backend and dependency contract after its qualified
runtime gate passes.
