# RFC 0253: Root `try` and Root `errdefer`

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started

## Summary

`try` and `errdefer` become valid at entry-module root. A root `try` whose
operand is an Error exits the program: active root cleanup runs (errdefers
included), stdout is flushed, the Error is reported to stderr in its existing
direct print form, and the process exits with status 1. No syntax is added.

## Motivation

The entry module's root body is the program. Today it cannot use `try`
(`type.try-requires-error-result`), so every script either wraps its body in a
function or matches each fallible call by hand:

```hexal
-- today
fun run(h: Heap): Nil | Error do
    let config = try Fs.open("config.toml", Fs.FileMode.Read())
    defer config.close()
    ...
    return nil
end
let outcome = run(Heap())
if outcome is Error then
    print(outcome, "\n")
    return 1
end
```

```hexal
-- proposed
let config = try Fs.open("config.toml", Fs.FileMode.Read())
defer config.close()
...
```

Zig (`pub fn main() !void`) and Rust (`fn main() -> Result<..>`) both let the
entry propagate, report the error, and exit with status 1. Odin's `main`
cannot propagate; `or_return` works only inside procedures whose last result
is the error, as Hexal's `try` does today.

## Design

### Root `try`

- `try` is valid in entry-module root scope, including root `if`, `while`,
  and `for` bodies, with the same operand rule as in a function: exactly one
  Error member and at least one success member. Its value is the normalized
  success value, as in a function.
- It stays invalid at imported-module root, inside any cleanup action, and
  inside a function whose result does not accept Error. All three keep their
  current diagnostics; `try requires an enclosing function whose result
  accepts Error` stays accurate at imported-module root, which has no
  enclosing function.
- When the operand's active member is Error, the root `try`:
  1. runs every active root cleanup as an Error exit, so root errdefers run
     with defers in shared reverse registration order;
  2. flushes stdout, so program output precedes the report;
  3. writes the report to stderr;
  4. exits with process status 1.
- Status 1 is also a status a root `return 1` can produce; the two exits are
  not distinguishable by status, as in Zig and Rust.
- A failed stderr write is ignored; the exit status is still 1. The report
  path allocates nothing and creates no further Error.
- A trap during cleanup ends the program as any trap does; the report is not
  written.
- A successful root `try` continues normally and runs no errdefer.
- Root exit does not join tasks (`docs/reference.md`, Concurrency), and root
  `try` exit inherits that.

### Root `errdefer`

- `errdefer` is valid in entry-module root scope under the same rule as root
  `try`. It runs only when the program exits through a root `try`; a root
  `return`, fallthrough, or trap never runs it.
- It stays invalid at imported-module root and in functions whose result does
  not accept Error, with the current diagnostic.

### Report format

One line: the Error's existing direct print form
(`file:line:column: header: message`), prefixed by `[Error] ` in the same
style as `[Runtime Error]` traps, followed by a newline:

```text
[Error] config.hex:30:12: not found: file not found
```

The location is the Error's construction site, which `Error(kind, message)`
and every runtime producer already record.

### Lowering

- `checkTryExpression` and `checkErrdeferStatement`
  (`compiler/checker/errors.go`) accept the entry-module root scope.
- `renderRootReturnStatement` (`compiler/generator/render_statements.go`)
  becomes one root-exit helper taking the exit status, the error-exit flag
  passed to `unwindAllDefers` (`"true"` runs errdefers unconditionally,
  `"false"` skips them), and an optional Error to report. Root `return` passes
  its status, `"false"`, and no Error; root `try` passes 1, `"true"`, and the
  Error.
- The report is emitted into the entry module, the only module a root `try`
  can appear in, through the existing per-module `print_error_direct`
  template (`compiler/generator/packages/module.h`). No `hexal/runtime.c`
  function is added, so a program without root `try` selects nothing new.

## Documentation

`docs/reference.md`, Programs (root `return`) and Errors: replace the rule that
`try` and `errdefer` are invalid at entry-module root with the root `try` and
root `errdefer` rules above, including the report line and status 1.

`GRAMMAR.ebnf`: no change.

## Swept code

None: no current code exists only because root `try` was invalid.

## Implementation plan

1. `compiler/checker/errors.go`: admit entry-module root scope in
   `checkTryExpression` and `checkErrdeferStatement`; imported-module root
   stays rejected.
2. `compiler/generator/render_statements.go`: extract the root-exit helper
   from `renderRootReturnStatement` (status, error-exit flag, optional Error);
   root `return` calls it with unchanged output.
3. Root `try` Error branch: call the helper with status 1, `"true"`, and the
   Error; emit the stdout flush and the `[Error] ` report line through
   `print_error_direct` in the entry module.
4. Integration tests 1-7; add fixtures 8-11 to `fixtureCatalog`.
5. `docs/reference.md` updates; remove this RFC's `docs/status.md` row on
   closure.
6. Rebuild the snippet manifest only if a snippet uses root `try` or root
   `errdefer`; no existing snippet can, so no artifact should move.

## Validation

Integration (`compiler/tests/integration/error_test.go`), asserting generated
C text:

1. A root `try` on `Int32 | Error` compiles; its value has the success type.
2. A root `try` inside a root `if` body and inside a root `for` body compiles.
3. A root `try` in an imported module's root is rejected with
   `type.try-requires-error-result`.
4. A root `errdefer` compiles; its action is emitted only on the root `try`
   exit path.
5. A root `errdefer` in an imported module's root is rejected with
   `type.errdefer-requires-error-result`.
6. The root `try` exit path emits the stdout flush before the report, and
   exit status 1.
7. A program with no root `try` emits no report code.

C23 lane (`compiler/tests/c23validation/`, as `fixtureCatalog` entries, so the
debug/release lane also proves both modes print the same report):

8. A root `try` on a user-constructed Error exits 1 and writes exactly the
   `[Error] ` line to stderr.
9. On root `try` exit, a root `defer` and a root `errdefer` run in reverse
   registration order before the report is written; stdout written before
   the `try` appears before the report.
10. A program whose root `try`s all succeed runs to its end, exits 0, and runs
    no `errdefer`.
11. A runtime-produced Error (`Fs.open` of a missing file) through a root
    `try` exits 1 with its report.

## Alternatives rejected

- **Error propagation traces.** Each Error would record the `try` sites it
  passed through, so the report could show which caller reached a shared
  failing helper. Rejected: the trace must live inside `Error` to stay
  mode-independent (generated C and stderr may not differ between build
  modes), which enlarges every `T | Error` result and copy, success paths
  included (about 40-72 bytes on an Error of about 432). Adding caller context
  to the message, `return Error(e.kind, "loading theme")`, answers the same
  question at no cost.

## Excluded review findings

- *The direct Error print form is the brace record.* Not reproduced:
  `module.h` defines `print_error_direct` as `file:line:column: header:
  message`; the brace form is `print_error_nested`, used only inside
  aggregates.
- *Root `try` needs flow-fact rules like root `return`.* Not needed: `try`
  yields the success type directly, so there is no narrowing to grant, and
  its Error branch leaves the program.
