# RFC 0182: Program Entry and Exit

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready after RFC 0186; design and execution plan
  settled, implementation not started
- Created: 2026-09-14
- Updated: 2026-09-15
- Scope: expose immutable process arguments and let the entry module select the
  process exit status
- Depends on: RFC 0186 and the current module, root-defer, Task-root, String,
  Slice, Error, target-profile, and native-bootstrap contracts
- Coordinates with: RFC 0178 for the shared `std/program` module and conditional
  `uv_setup_args`, closed RFC 0184 for completed-call print output, and ADR 0055 for
  executable invocation
- Does not add: a required user `main`, environment mutation, immediate process
  termination, root Error propagation, or implicit resource cleanup

## Summary

The selected entry module remains the program body. Add:

```text
import
    Prog from "std/program"
end

Prog.arguments() -> Slice<String> | Error
```

At entry-module scope, `return` records one `UInt8` process status and enters
the ordinary root cleanup path. Bare return and fallthrough mean zero.

```hexal
import
    Prog from "std/program"
end

args := Prog.arguments()
if args is Error then
    print(args)
    return 1
end
if args.length() < 2 then
    return 2
end
print(args[1])
```

After the first `if`, `args` is narrowed to `Slice<String>` because the only
alternative path ends in a root `return`.

## Selected design

| Question | Decision | Rationale |
| --- | --- | --- |
| Exit spelling | Root `return` | Reuses structured control flow and preserves defer cleanup. |
| Status type | Exact `UInt8` | Gives every initial target one portable `0..255` source contract. |
| Argument storage | Demand-driven immutable snapshot | Programs that never request arguments pay no conversion or storage cost. |

The final root expression remains discarded. No `exit` library function is
added.

## Source surface

```text
arguments() -> Slice<String> | Error
```

- `arguments` is an exported function of `std/program` and is available only
  through an ordinary file-local import alias. This RFC adds no protected
  `Program` name.
- RFCs 0178 and 0182 extend RFC 0186's same core-library declaration table and
  generated program component.
- `arguments()` returns the host invocation in order, including element zero
  when supplied by the host. Zero arguments produce an empty Slice.
- Repeated successful calls return equivalent views over the same immutable
  process-lifetime snapshot. Repeated failed calls return the same failure
  classification with the current call site's source location.
- The Slice and String bytes are read-only. Callers neither mutate nor free
  their backing storage.
- No snapshot, conversion buffer, or argument component is emitted solely for
  a program that cannot reach `std/program.arguments()`. Demand is based on checked
  reachability, not whether a runtime branch happens to execute; reachable but
  unexecuted use still selects and initializes the snapshot.

## Argument storage and ownership

- Snapshot storage is private runtime memory retained until process return.
  This bounded process-lifetime allocation is intentional: the API takes no
  caller Heap because the returned view has process lifetime and is shared by
  every call.
- Do not free the snapshot in the root epilogue: an abandoned detached Task may
  still hold the view until process termination. The operating system reclaims
  it; sanitizer coverage records this one runtime-lifetime allocation narrowly.
- Each argument is represented by an ordinary `hex_string` header over copied
  UTF-8 bytes and marked non-owning through the existing static storage kind.
  The Slice contains those String handles in host order.
- The bytes contain one trailing NUL for C interoperability, but Hexal length
  excludes it. Host argument ABIs cannot carry an embedded NUL.
- Rune length is computed while validating or converting each argument.
- Partial initialization frees every private allocation before publishing a
  stable failed state. Initialization is once-only and completes before Tasks
  or module statements can observe it.
- `String.free` on a runtime-proven non-owning String traps with
  `[Runtime Error] cannot free a non-owning String`. The existing checker error
  for a statically known literal remains the more specific
  `cannot free a String literal`.

## Argument conversion

### POSIX

- Use the entry adapter's `argc` and authoritative `argv` in order.
- Validate every argument as UTF-8 before publishing any snapshot.
- Invalid bytes return `ErrorKind.InvalidInput()` with message
  `program argument is not valid UTF-8`.
- Copy the bytes; do not retain host `argv` pointers.
- Conversion is all-or-nothing. One invalid argument makes the complete
  snapshot unavailable; no partial argument Slice is exposed.

### Windows

- Read `GetCommandLineW()` and split it with `CommandLineToArgvW`; do not
  reproduce Windows quoting rules in Hexal.
- Preserve `CommandLineToArgvW` behavior for argument zero, whose quoting rules
  differ from later elements. If the native command line is empty, its returned
  executable-path element is the single argument.
- Convert each UTF-16 argument with
  `WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, ...)`, using checked
  allocation-size arithmetic.
  An unpaired surrogate returns `ErrorKind.InvalidInput()` with message
  `program argument is not valid Unicode`.
- Release `CommandLineToArgvW` storage with `LocalFree` after copying.
- Selecting arguments adds the required Windows system-header and Shell32 link
  dependency. No such dependency is emitted when arguments are unreachable.

Allocation failure is `ErrorKind.ResourceExhausted()`. Any other native setup
failure uses the common native ErrorKind mapping. Both use fixed message
`program arguments unavailable`. The runtime stores only the stable failure
kind; the generated call adapter adds its module, line, and column without an
additional allocation.

## Root-return contract

- `return` at entry-module scope exits the complete entry module after active
  root defers. It remains invalid at top level in imported modules.
- `return expression` requires exact `UInt8` after ordinary literal contextual
  typing. There is no implicit signed, wider-integer, union, or floating
  conversion.
- `return` without an expression and entry-module fallthrough both record zero.
- Status literals `0` through `255` are valid. An out-of-range literal uses the
  ordinary UInt8 range diagnostic.
- A root return under `if`, `while`, or `for` exits the program body;
  statements proven unreachable afterward follow the existing unreachable-
  statement rule. Match arms are expressions, so a root `return` cannot appear
  inside an arm.
- A root `return` is a context-valid terminating statement for flow facts,
  exactly like a function `return`: an `is`/nil fact established on the
  continuing path survives an `if` whose alternative ends in a root `return`.
- A root `return` inside nested scopes runs every active `defer` from the
  innermost scope outward, then the root scope's defers, reusing function-return
  defer unwinding. The status value is evaluated once before any defer runs;
  no defer can change it, because `defer` accepts an expression and root
  `return` is a statement.
- A return inside a function or method retains that declaration's ordinary
  result contract; entry-module status rules do not leak into it.
- `try` and `errdefer` remain invalid at root because the root has no Error
  result. A trap remains abnormal termination and need not run defers.
- A trap's native process status is unspecified and may numerically equal a
  valid user-returned UInt8 status. Exact trap stderr, not a reserved exit code,
  identifies abnormal Hexal termination.

The exact imported-module diagnostic is:

```text
return is valid only in the entry module or a function body
```

The exact wrong-type diagnostic is:

```text
entry-module return requires UInt8; got <Type>
```

## Program lifecycle

1. The generated entry adapter is emitted when `std/program.arguments()` or
   `std/program.executable_path(heap)` is reachable, and never otherwise.
2. When executable-path demand exists, native bootstrap runs first and then
   `uv_setup_args` runs exactly once, following libuv's documented requirement
   that it precede `uv_exepath` on every platform. POSIX uses
   `argv = uv_setup_args(argc, argv)` and the returned pointer is the
   authoritative invocation. Windows calls `uv_setup_args(__argc, __argv)` with
   the MinGW CRT globals and does not use the result, because Windows argument
   text comes from `GetCommandLineW`. Argument demand alone selects no libuv,
   bootstrap, or `uv_setup_args`.
3. If `std/program.arguments()` is reachable, its immutable snapshot completes or
   records failure before any module statement and before scheduler startup.
4. Imported modules contribute declarations and storage only; they have no
   runtime initializer or executable top-level statements.
5. The entry module runs as the root Task when the scheduler is selected and
   directly otherwise.
6. Fallthrough or root return records the status and enters one epilogue.
   Active root defers run in reverse registration order.
7. Every completed print call has already committed through closed RFC 0184; there is
   no persistent compiler-owned stdout buffer and no shutdown flush. Ordinary
   File, Pipe, socket, Channel, Mutex, and user allocations are not implicitly
   closed, freed, joined, or waited upon.
8. Selected runtime components perform only the shutdown required for safe
   process return. Detached Tasks and native work remain abandoned under the
   existing root contract. An unfinished joinable Task is abandoned exactly
   like an unfinished detached Task; root return does not imply an automatic
   join. The C entrypoint then returns the recorded status.

No Error is implicitly printed or converted to a process status.

## C and target lowering

- Windows always retains `int main(void)`. Argument text comes from
  `GetCommandLineW`; executable-path setup reads the CRT's `__argc`/`__argv`
  globals, so the signature never widens.
- A POSIX program reaching `arguments()` or `executable_path()` uses
  `int main(int argc, char **argv)`. Other POSIX programs retain
  `int main(void)`. Host pointers remain private to the root C file.
- Host-neutral output (empty `Project.Target`) keeps both platform paths chosen
  at C-compile time, like every runtime component. When either demand exists,
  the root C file spells the entry under `#if defined(_WIN32)` as
  `int main(void)` and otherwise as `int main(int argc, char **argv)`; without
  demand it emits one unconditional `int main(void)`. An explicit target profile
  emits only its selected signature.
- `std/program.arguments()` selects demand-driven `hexal/program.h` and
  `hexal/program.c`. RFC 0178 extends the same component for other program
  operations; it does not create another Program runtime.
- Root lowering owns one `uint8_t` status initialized to zero and one cleanup
  label. Every root return evaluates its value once, assigns the status, and
  jumps to that label.
- With Task selected, the common epilogue calls `hex_task_complete` before C
  returns. Without Task, it performs the same defer/output ordering directly.
- Final C returns `(int)status`. No generated module header exposes `argc`,
  `argv`, wide strings, or host ABI types.
- Equivalent sources and Project values remain deterministic; host argument
  contents affect runtime state, never generated artifacts.

## Required sweep

- parser module-scope return rejection and recovery;
- checker entry/import context, return typing, and unreachable flow;
- checked root-return representation and fail-closed generator validation;
- root defer lowering, return labels, and Task completion;
- generated `main(void)` spelling and demand-driven host-invocation adapter;
- String static-storage runtime diagnostic wording;
- program-component selection, Windows link dependencies, and source mapping;
- workbench and driver process invocation;
- snippets, manifest, and tagged runtime fixtures; and
- RFC 0178 coordination so only one `std/program` declaration table, component, native
  bootstrap, and `uv_setup_args` call exists.

## Detailed implementation plan

### Phase 1: record the baseline

1. Freeze generated root C for fallthrough, root defer, Task completion, the
   absence of a shutdown print flush, and every qualified profile.
2. Record the current module-scope return diagnostic and artifact set.
3. Record manifest hashes before changing entry lowering.

### Phase 2: checked representation

1. Let the parser represent a module-scope return instead of rejecting it
   before the entrypoint is known.
2. Pass entry-module identity into checking; accept root return only there and
   emit the exact imported-module and status diagnostics above.
3. Add a distinct checked root-return statement. Do not overload a function
   return with an absent function result.
4. Extend generator validation and walking exhaustively for the new checked
   statement.

### Phase 3: std/program metadata and demand

1. Extend RFC 0186's `std/program` core-library declaration table with exported
   module function `arguments`; add no protected type.
2. Discover argument and executable-path demand program-wide.
3. Select one program component, the target-specific entry signature, native
   bootstrap, conditional `uv_setup_args`, and Windows Shell32 dependency
   independently and exactly.

### Phase 4: argument runtime

1. Add the program component templates and one immutable snapshot record,
   initialized exactly once by the entry adapter before source execution. Do
   not add lazy initialization, a lock, or an atomic state machine.
2. Implement POSIX copying/validation and Windows native splitting/conversion.
3. Publish only a complete immutable snapshot or a stable failure kind.
4. Return source-located Errors without allocating on the failure path.
5. Update non-owning String free handling without changing owned String cleanup.

### Phase 5: root lowering

1. Emit one status slot and cleanup label in the root C function.
2. Route bare, valued, and nested-control-flow root returns to the label.
3. Run root defers, Task completion, and final C return in the lifecycle order
   above. Emit no fictional stdout finalization step.
4. Keep ordinary function and method return lowering byte-identical.

### Phase 6: conformance and documentation

1. Add focused parser/checker/generator tests and public integration tests for
   every Validation item.
2. Add tagged runtime fixtures for argument conversion, status, defers, Task
   root completion, demand absence, and exact failure behavior.
3. Compile and run every fixture under each qualified target gate.
4. Regenerate only manifest entries changed by the new entry or root-return
   surface and inspect the artifact breakdown.
5. Update `docs/reference.md` only after behavior stabilizes and only with
   explicit user approval.

## Validation

This list is exhaustive:

- zero, one, empty, non-ASCII, invalid-POSIX-UTF-8, and many arguments in exact
  host order;
- Windows argument-zero quoting, ordinary quoting, empty command line, empty
  arguments, strict UTF-16 conversion, and non-ASCII arguments;
- repeated success and failure calls, source-local failure provenance, partial-
  initialization cleanup, immutable process lifetime, and non-owning free
  rejection;
- checked-reachability demand, including selection for reachable but unexecuted
  use, and absence of snapshot, program component, Windows dependencies, and
  widened entry signature when unreachable;
- Windows `main(void)` in every case; POSIX `main(argc, argv)` exactly when
  arguments or executable path are reachable; exactly one native bootstrap and
  one `uv_setup_args` call, before the first `uv_exepath`, only when executable
  path is reachable (Windows through `__argc`/`__argv`); argument demand alone
  selects no libuv or `uv_setup_args`;
- root fallthrough, bare return, statuses 0 and 255, and rejected negative,
  out-of-range, wider, signed, floating, and union statuses;
- root return under `if`, `while`, and `for`, including unreachable
  statements afterward;
- a union narrowed after `if value is Error then ... return 1 end` is used as
  its remaining member without further narrowing;
- `defer cleanup()` followed by a root `return 7` inside a nested `if`, with a
  later `return 3`, exits with 7, runs nested and root defers in reverse order,
  and never exits with 3 or 0;
- host-neutral output spells the entry under `#if defined(_WIN32)` only when
  arguments or executable path are reachable;
- reverse root-defer order, absence of shutdown output flushing, trap bypass,
  unspecified trap status, Task completion, and detached and unjoined-Task
  abandonment;
- unchanged function/method returns and continued root rejection of `try` and
  `errdefer`;
- imported-module rejection with the exact source-mapped diagnostic;
- one `std/program` declaration table and generated component when combined
  with RFC 0178 operations;
- no public native ABI types and deterministic generated artifacts; and
- exact generated-C compilation, stdout/stderr, and process status under every
  qualified target gate.

POSIX items (argument conversion, invalid POSIX UTF-8, and POSIX entry
signatures) are verified as generated-text assertions over host-neutral output
until a POSIX target profile is qualified. No POSIX runtime branch is claimed
as executed.

## Reference synchronization

Do not edit `docs/reference.md` from this specification. Approved
implementation updates the grammar, `std/program` arguments, root return, String
non-owning cleanup, lifecycle ordering, and C entry contract only after behavior
stabilizes and with explicit user approval. It also removes the unsupported
claim that shutdown flushes a compiler-owned stdout buffer, unless closed RFC
0184's reference synchronization already removed it; closed RFC 0184 owns
whole-call output atomicity.
