# RFC 0253: Root `try` and Error Propagation Traces

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft. Open questions 1-3 need the author's decision before
  implementation.

## Summary

Two additions to error handling, neither adding syntax:

1. **Root `try`.** `try` and `errdefer` become valid at entry-module root. A
   root `try` whose operand is an Error exits the program: active root
   cleanup runs (errdefers included), the Error and its trace are reported to
   stderr, and the process exits with status 1.
2. **Propagation traces.** Every Error records the `try` sites it propagated
   through, in a fixed-capacity inline trace. The root `try` report prints it.
   Construction site stays the Error's own `file:line:column`, as today.

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
entry propagate and have the runtime report and exit nonzero.

An Error records only where it was built. When it surfaces three calls up, the
path it took is lost. Zig's error return trace records exactly that path.

## Constraint: no debug-only behavior

Zig records return traces only in Debug and ReleaseSafe. Hexal cannot:
`docs/reference.md` Build modes requires generated C to be byte-identical in
both modes and forbids a mode from changing stderr. A debug-only trace breaks
both rules. So the trace is **language semantics, on in every mode**, and its
cost is bounded so that is acceptable:

- Recording happens only on the Error branch of a `try`, never on success.
- Each record is one pointer store to a static, compiler-emitted site record.
- Capacity is fixed and inline, so there's no allocation and nothing to free.
  "An Error owns nothing" still holds.

## Design

### Root `try`

- `try` is valid in entry-module root scope, including root `if`, `while`,
  and `for` bodies, with the same operand rule as in a function: exactly one
  Error member and at least one success member.
- It stays invalid at imported-module root (which has no exit),
  inside any cleanup action, and inside a function whose result does not
  accept Error. All three keep their current diagnostics.
- When the operand's active member is Error, the root `try`:
  1. appends its own site to the Error's trace;
  2. unwinds every active root cleanup exactly as a root `return` does, but
     as an Error exit, so root errdefers run in the shared reverse order with
     defers;
  3. writes the report below to stderr;
  4. exits with process status 1 through the same exit path as root
     `return 1`, including its stdout handling.
- A trap during cleanup ends the program as any trap does; the report is not
  written.

Report format: one line in the direct Error print form, prefixed like runtime
traps, then one line per trace entry, innermost first:

```text
[Error] app.hex:12:15: not found: config missing
    propagated at lib/config.hex:30:17
    propagated at app.hex:4:14
```

When more propagations happened than the trace holds, a final line
`    ... <n> more` gives the count that was not recorded.

### Root `errdefer`

- `errdefer` is valid in entry-module root scope under the same rule as root
  `try`. It runs only when the program exits through a root `try`; a root
  `return`, fallthrough, or trap never runs it.
- It stays invalid at imported-module root and in functions whose result does
  not accept Error, with the current diagnostic.

### Propagation traces

- Every Error carries a hidden trace: up to `ErrorTraceCapacity` (8, open
  question 1) propagation sites, plus a count of every propagation, including
  ones not recorded.
- `Error(kind, message)` starts with an empty trace. Runtime-produced Errors
  do too.
- Each `try` whose operand is Error appends its own site
  (`file:line:column` of the `try` keyword) before the Error leaves the
  function. When the trace is full, only the count increases, so the recorded
  sites are the ones nearest the origin.
- A `return` of an Error value does not record a site, nor does anything
  else; only `try` does (open question 2). Re-wrapping with
  `return Error(e.kind, "context")` creates a new Error with an empty trace.
- The trace is not a member. It is invisible to Hexal code, to `==`/`!=`
  (two Errors differing only in trace stay equal), and to `print` (both the
  direct and nested Error forms are unchanged). Its only consumer is the root
  `try` report (open question 3).
- Copying an Error copies its trace. Sites are static data and are never
  freed.

### C lowering

- `compiler/generator/packages/error.h` gains:

  ```c
  typedef struct hex_error_site {
      const hex_string *file;
      size_t line;
      size_t column;
  } hex_error_site;
  ```

  `hex_t_Error` gains `const hex_error_site *hex_trace[ErrorTraceCapacity]`
  and `size_t hex_trace_count`. There is one static inline push helper:
  it stores the site when `count < capacity`, then increments the count
  with `ckd_add` (`<stdckdint.h>`), saturating at `SIZE_MAX`.
- Each `try` site emits one `static const hex_error_site` in its module `.c`,
  and the Error branch of `hoistTry` (`compiler/generator/errors.go`) calls
  the push helper on the propagated Error before returning.
- Root `try`'s Error branch pushes its site, sets exit status 1, writes the
  report through one runtime function in `hexal/runtime.c`, and reuses
  `renderRootReturnStatement`'s unwind (`render_statements.go:403`), passing
  `unwindAllDefers(..., "true")` so root errdefers run.
- `Error` equality excludes the two trace fields.
- `ErrorTraceCapacity` lives in `compiler/config` beside the other capacity
  constants. It is a resource bound, not a measured optimum.

### Documentation

`docs/reference.md`:

- Remove the rule that `try` and `errdefer` are invalid at entry-module root
  (lines 884-885, 1032-1033); add the root `try` and root `errdefer` rules.
- In Errors, add the trace rules, and state that the trace is excluded from
  `==` and from `print`.
- Update the Error size statements (lines 975 and 2232) to the measured
  `size_of<Error>()` after the change. The number is measured, never
  estimated.
- In Build modes, no change: this design keeps the mode-independence rule.

`GRAMMAR.ebnf`: no change.

### Swept code

None: no current code exists only because root `try` was invalid.

## Validation

Integration (`compiler/tests/integration/error_test.go`), asserting the text
of the generated C:

1. A root `try` on `Int32 | Error` compiles; its value is the success member.
2. A root `try` in a root `if` body compiles.
3. A root `try` in an imported module's root is rejected with
   `type.try-requires-error-result`.
4. A root `errdefer` compiles; its action is registered on the error exit
   path only.
5. A root `errdefer` in an imported module's root is rejected with
   `type.errdefer-requires-error-result`.
6. A function-level `try` Error branch calls the push helper with a static
   site whose line and column are the `try` keyword's.
7. `Error == Error` on two Errors with equal fields but different traces
   compares only the non-trace fields in generated C.

C23 lane (`compiler/tests/c23validation/`, tagged `c23`), executing the
program:

8. A root `try` on an Error propagated through two function `try`s exits 1
   and writes exactly the report: the origin line plus two `propagated at`
   lines, innermost first.
9. An Error propagated through `ErrorTraceCapacity + 2` `try`s reports
   `ErrorTraceCapacity` sites and `... 2 more`.
10. On root `try` exit, a root `defer` and a root `errdefer` both run, in
    reverse registration order, before the report is written. On
    fallthrough, the `errdefer` does not run.
11. `print(error)` output is byte-identical to before this change.

## Open questions

1. **Capacity.** Recommendation: 8. Each slot costs one pointer, so 8 adds
   about 72 bytes to every Error-carrying result. Keeping the first N (nearest
   the origin) was chosen over the last N because the origin's neighborhood
   usually holds the cause; the report still shows the total.
2. **Should a plain `return e` record too?** Only `try` records in this
   draft. `if r is Error then return r end` is a hand-written
   propagation that would then leave no trace entry. Recording on
   every `return` of a statically Error-typed value is cheap. Recording on
   an unnarrowed `T | Error` return adds a tag test to every such return.
3. **Expose the trace to Hexal code?** Currently only the root report reads
   it. A method (`error.trace()` returning the sites) would let long-running
   programs log traces without exiting. Recommendation: not in this spec;
   add it when a server or logging use appears.
