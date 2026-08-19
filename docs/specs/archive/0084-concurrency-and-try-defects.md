# RFC 0084: Concurrency and `try` Lowering Defects

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-19. C1 and C2 shared one cause: the
  statement hoisters recursed into nested statement bodies, emitting a
  function-scope copy of the prologue before the block's own copy. Deleting
  that recursion from `hoistTryInStatement` (`compiler/generator/errors.go`)
  and `hoistConcurrencyInStatement` (`compiler/generator/concurrency.go`)
  fixes both — the `for` shape's `Unknown Error` disappeared with the hoisted
  copy that referenced the not-yet-declared loop binder, and the Evidence
  program now calls `mk` once with the prologue inside the `if`. C3 maps the
  POSIX fiber stack with `mmap`, `mprotect(PROT_NONE)` on one guard page at
  the low end, and sets `ss_sp`/`ss_size` to the usable region above it,
  freed with `munmap` (`compiler/generator/packages/concurrency.c`). The
  spawn-count table, the untaken-branch structural assertion, the counter
  loop, the `for` shape, and the guard-page text are all pinned by tests;
  `docs/status.md` records the guard fault itself as unverified. The snippet
  manifest moved only the ten task snippets' `hexal/concurrency.c` entries —
  the catalog contains no `try` in a nested block, so C1/C2 churned nothing.
- Created: 2026-08-19
- Updated: 2026-08-19
- Scope: three defects found while validating parallel execution — duplicated
  `try` prologues, `try spawn` inside `for`, and missing POSIX fiber guard pages
- Depends on: nothing
- Coordinates with: `docs/reference.md`, `docs/status.md`
- Does not change: the scheduler model, the M:N design, or the Task/Channel/
  Mutex/Atomic surface

## Summary

Writing one CPU-saturating example surfaced three defects. Two are miscompiles
that produce C which compiles cleanly and behaves wrongly; one is a runtime
safety property the reference claims and the runtime does not provide.

| # | Defect | Class |
|---|---|---|
| C1 | `try` inside a nested block emits its prologue twice | silent miscompile |
| C2 | `try spawn` inside a `for` loop fails with `Unknown Error` | compiler bug |
| C3 | POSIX fiber stacks have no guard page, contrary to `reference.md` | unsound |

All three were found by probe against current `main`, not inferred.

## C1 — `try` inside a nested block runs twice

### Evidence

```hexal
fun mk(): Int32 | Error do
    return 1
end

fun run(): Int32 | Error do
    if true then
        v: Int32 = try mk()
        print(v)
    end
    return 0
end
```

Generated C:

```c
hex_f_m3_app_run(void) {
    const ... hex_try_1 = hex_f_m3_app_mk();        /* hoisted, OUTSIDE the if */
    if (hex_try_1.tag == ...member_1) {
        return ...;                                  /* propagates on error */
    }
    if (true) {
        const ... hex_try_2 = hex_f_m3_app_mk();    /* the real one */
        if (hex_try_2.tag == ...member_1) { return ...; }
        const int32_t hex_v_v = hex_try_2.payload.member_0;
        ...
    }
}
```

The prologue is hoisted to **function scope** rather than to the enclosing
statement position. The call therefore executes twice on the taken path, and the
hoisted copy executes unconditionally — including when the branch is never
taken, since it precedes the `if` entirely.

### Consequences

- **Side effects run twice.** `try f()` calls `f` twice whenever it appears in a
  nested block.
- **An untaken branch still executes its `try`.** The hoisted copy is outside the
  conditional.
- **`try spawn` leaks a task.** In a loop spawning 8 workers, 9 tasks are
  created. The hoisted one is never joined or detached, which violates
  `reference.md`'s "Exactly one successful join or detach is allowed across
  aliases", never reclaims its result storage, and occupies a worker doing
  discarded work.

### Scope, probed

| Shape | `hex_task_spawn(` emitted |
|---|---|
| `try spawn` at function body top level | 1 — correct |
| `try spawn` inside `if` | **2** |
| `try spawn` inside `while` | **2** |
| plain `try` inside `while` | **2 calls** |

Not loop-specific and not spawn-specific: any `try` in any nested block.

### Fix

Hoist the `try` prologue to the enclosing **statement position within its own
block**, not to the function body. The in-place rendering is the correct one;
the function-scope copy must not be emitted.

`AGENTS.md` describes the intended behaviour already — the generator hoists a
`try` prologue so the operand renders as a value — and the defect is that the
hoist target is the wrong scope.

**The defect shape is two hoists, not one misplaced hoist.** A pre-pass emits
the prologue into the function-level builder, and the nested render emits it
again into the block's own builder with a fresh counter — which is why the two
copies have different names (`hex_try_1`, `hex_try_2`) rather than being one
statement in the wrong place. Deleting the function-level copy is therefore the
fix; relocating it is not, because the correct copy already exists. Confirm both
emission sites before editing either.

### Test

- The Evidence program emits exactly one call to `mk`.
- `try spawn` in `if`, `while`, nested `if` inside `while`, and at top level each
  emit exactly one `hex_task_spawn`.
- A `try` inside a branch that is not taken performs no call. Assert on generated
  C structure: the prologue appears inside the block that contains the `try`.
- A counter-based integration case: N spawns in a loop produce exactly N tasks.

## C2 — `try spawn` inside a `for` loop is an `Unknown Error`

```hexal
fun run(): Int64 | Error do
    a: Array<Int64, 3> = [1, 2, 3]
    for v in a do
        w: Task<Int64> = try spawn burn(v)
        w.join()
    end
    return 0
end
```

```
[Unknown Error] variable is not present in checked bindings
```

Per `AGENTS.md`, an `Unknown Error` means the compiler could not classify the
failure and the problem is in the compiler, not the program. This program is
valid: the same body inside a `while` loop compiles.

The message points at binding resolution for the `for` binder interacting with
the `try` prologue, which makes C1 the likely shared cause — the hoisted
prologue references the loop binder from a scope where it is not yet declared.

### Fix

Expected to fall out of C1: with the prologue emitted inside the loop body, the
binder is in scope. **Verify rather than assume** — if the failure persists after
C1, it is an independent binding-resolution defect and needs its own diagnosis.

### Test

The program above compiles and emits exactly one `hex_task_spawn`, matching the
`while` form. Add the `for` shape to whatever covers C1's spawn counts.

## C3 — POSIX fiber stacks have no guard page

`docs/reference.md:886` states:

> Stacks reserve 1 MiB including guard page.

The POSIX backend allocates with plain `malloc` and never calls `mprotect`
(`compiler/generator/packages/concurrency.c`, zero occurrences):

```c
static hex_context_impl *hex_context_create(void (*entry)(void *), void *param) {
    const size_t stack_size = 1u << 20;
    ...
    context->stack = malloc(stack_size);
    ...
    context->context.uc_stack.ss_sp = context->stack;
    context->context.uc_stack.ss_size = stack_size;
```

A fiber that overflows its stack writes into adjacent heap memory silently.
There is no fault, no trap, and no diagnostic — the failure mode is
indistinguishable from heap corruption, and it scales with fiber count.

Windows is correct: `CreateFiberEx` supplies a guard page from the PE stack
reservation.

### Fix

Allocate the POSIX stack with `mmap` plus an `mprotect(PROT_NONE)` guard page at
the low end, and set `ss_sp`/`ss_size` to the usable region above it. Free with
`munmap`.

This is the standard construction and needs no new abstraction. The reference
sentence then becomes true on both targets rather than one.

### Test

Because no test executes generated C, assert textually that the POSIX branch of
the concurrency runtime contains a `PROT_NONE` mapping adjacent to each fiber
stack, and that the usable size excludes it. Record under
`docs/status.md`'s known coverage gaps that the fault itself is unverified.

## Invariants

1. The M:N scheduler model, worker count, Task/Channel/Mutex/Atomic surface, and
   `Task.yield()` requirement are unchanged.
2. C1's fix changes generated C only where a `try` appears in a nested block. A
   `try` at function-body top level is byte-identical.
3. No program that compiles today and is unaffected by C1–C3 changes behaviour.
4. C3 changes the POSIX runtime template only; Windows output is unchanged.
5. Every operand still evaluates exactly once, in source order.

## Validation

- The spawn-count table above, as tests: each shape emits exactly one
  `hex_task_spawn`.
- The `for` shape from C2 compiles.
- `go test ./...`, `go vet ./...`.
- The snippet manifest moves for snippets containing a `try` in a nested block,
  and for no others. Enumerate them before the change so the diff is checkable.
- `grep -c mprotect` over the concurrency runtime returns non-zero.

## Non-goals

- Changing stack size, adding growable stacks, or changing fiber scheduling.
  RFC 0085 owns stack sizing if it is taken up.
- Preemption, work stealing, or scheduler fairness.
- Making `Unknown Error` unreachable in general — C2 fixes one instance.
- Executing generated C in tests. The known coverage gap stands, and these three
  defects are further evidence for it.

## Drawbacks

- C1 changes generated C for a common construct, so the manifest churn is wide
  even though the change is a correction.
- C3 makes the POSIX runtime slightly more complex than `malloc`, and `mmap`
  with a guard page is one more platform assumption — though a portable one
  across every POSIX target the reference already claims.

## Note on discovery

All three were found while writing a single example program to demonstrate
parallelism, and every one of them produces C that compiles. They are the third
independent instance — after RFC 0073's D2 and D33 — of a defect class that the
entire Go test suite cannot observe. That is a data point for whoever revisits
the generated-C compile gate, and it is recorded here rather than argued.

## Closing notes

- C2 verified as fallen out of C1, not independently diagnosed: the probe
  failed with the recorded `Unknown Error` before the fix and compiles with
  one `hex_task_spawn` after it, matching the `while` form.
- The catalog-wide snippet enumeration before the change found no snippet
  with a `try` in a nested block, so C1/C2 moved no manifest entry; C3 moved
  the `hexal/concurrency.c` entry of exactly the ten task snippets.
- The `for` shape's join discards its result; `Task.join()` as a discarded
  call statement is the shape the probe used.
