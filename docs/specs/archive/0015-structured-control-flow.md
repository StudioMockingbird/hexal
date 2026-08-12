# RFC 0015: Structured Control Flow

- Status: Implemented; conformance verified 2026-08-09
- Features: `if`/`elseif`/`else`/`end`, `while`/`end`, `break`, `continue`,
  lexical block scopes, Boolean condition checking, definite-return analysis,
  direct C23 lowering
- Created: 2026-08-09
- Depends on: RFC 0008 (functions and methods), RFC 0009 (core operators),
  RFC 0011 (structured checked expressions)
- Coordinates with: RFC 0014 (general type expressions and union types)
- Extends when accepted: RFC 0008's return-completeness rule and body
  statement grammar

## Summary

Seawitch adds structured conditional and loop statements. Blocks are delimited
by the existing `end` keyword; newlines remain whitespace and no terminator is
required.

```seawitch
if score >= 90
    grade: Int32 = 1
elseif score >= 80
    grade: Int32 = 2
else
    grade: Int32 = 3
end

while remaining > 0
    remaining = remaining - 1
end
```

`if` selects exactly one branch. `elseif` branches are tested in source order,
and `else` runs only when no condition is true. `while` evaluates its condition
before every iteration. Conditions must have type `Bool`; Seawitch does not
convert integers, pointers, or other values to Boolean conditions.

`while` execution can leave the loop when its condition becomes false or when
`break` exits its innermost loop. `return` exits the enclosing function, and
`continue` skips to the next condition evaluation.

The statements are available at module level and inside function or method
bodies. A block may contain declarations, assignments, calls, returns, nested
conditionals, nested loops, and loop-control statements. Function and method
declarations remain module-level only.

## Motivation

RFC 0008 makes functions executable but currently limits bodies to a flat list
of statements. Programs need branching and repetition before functions can
express ordinary algorithms. The feature must preserve the language's small
surface and compile to readable C23 without introducing a runtime control-flow
representation or an analyzer pass.

The syntax deliberately uses one `elseif` keyword and one closing `end` for an
entire conditional. A nested `if` remains available when a separate lexical
block is wanted:

```seawitch
if ready
    if valid
        use()
    end
end
```

There is no `then`, `do`, brace-delimited control-flow syntax, conditional
expression, labeled loop control, or loop result in this RFC.

## Syntax

### Conditional statement

The minimal form is:

```seawitch
if condition
    statement
end
```

Any number of `elseif` clauses may follow the first body. At most one `else`
clause may appear, and it must be last:

```seawitch
if condition_a
    first()
elseif condition_b
    second()
elseif condition_c
    third()
else
    fallback()
end
```

`elseif` is a single keyword. `else if` is not alternate spelling for an
`elseif` clause; it starts a nested `if` statement and therefore requires a
second matching `end`.

An empty branch is syntactically valid. This keeps the block grammar uniform
and matches C's ability to represent an empty compound statement:

```seawitch
if condition
elseif other_condition
else
end
```

The checker still validates every condition in an empty or unreachable branch.

### While statement

```seawitch
while condition
    statement
end
```

The body may be empty and may contain another `while` or `if`. There is no
implicit loop variable or loop result.

### Loop-control statements

`break` exits the innermost enclosing `while`:

```seawitch
while active
    if done
        break
    end
    work()
end
```

`continue` skips the remainder of the innermost enclosing `while` body and
reevaluates that loop's condition:

```seawitch
while index < limit
    index = index + 1
    if skip
        continue
    end
    process(index)
end
```

Neither statement takes an expression or produces a value. Both target the
nearest enclosing `while`, even when written inside nested `if` statements.
They cannot target a loop outside the current function or method. A function
or method declaration cannot be nested inside a loop, so no additional
function-boundary rule is needed.

### Statement positions

Both statements are valid wherever an executable statement is valid today:

- module-level statements, which lower into `main`; and
- function and method body statements.

They do not create declarations at module scope and cannot contain `fun`,
`impl`, or `type` declarations. A `return` inside either statement returns
from the enclosing function or method; it is still invalid at module level.
`break` and `continue` are parsed as statements but are valid only inside a
`while` body.

## Reference-level explanation

### Grammar

The executable statement grammar becomes:

```ebnf
top-level-item       = type-declaration | declaration | assignment
                     | function-declaration | impl-declaration
                     | call-statement | if-statement | while-statement
                     | break-statement | continue-statement ;

statement            = declaration | assignment | call-statement
                     | return-statement | if-statement | while-statement
                     | break-statement | continue-statement ;

if-statement         = "if" , expression , block
                     , { "elseif" , expression , block }
                     , [ "else" , block ] , "end" ;

while-statement      = "while" , expression , block , "end" ;

break-statement      = "break" ;
continue-statement   = "continue" ;

block                = { statement } ;
```

`function-declaration`, `impl-declaration`, `declaration`, `assignment`,
`call-statement`, `return-statement`, and `expression` retain the productions
owned by the earlier RFCs. In particular, `return-statement` remains valid only
inside a function or method body after parser context checking.

The parser must treat `elseif`, `else`, and `end` as block delimiters while
parsing a block. A delimiter belongs to the nearest still-open construct:

```seawitch
if outer
    while inner
        if nested
        end
    end
end
```

No newline-sensitive rule is added. The first token after a condition is the
first token of its body, and a block ends only at its structural keyword. A
missing `end` is therefore a syntax error at the location where the parser can
prove that the enclosing construct is incomplete, normally EOF or an outer
delimiter.

The parser uses construct-specific block boundaries:

1. an `if` body stops at `elseif`, `else`, `end`, or EOF;
2. an `elseif` body stops at `elseif`, `else`, `end`, or EOF;
3. an `else` body stops at `end` or EOF; and
4. a `while` body stops at `end` or EOF.

`elseif` and `else` encountered while a `while` body is open are structural
errors, not statements belonging to an outer conditional. An `elseif` or
`else` after an `else` is likewise an error on the enclosing `if`. An `end`
encountered with no open function, method, `if`, or `while` is an unexpected
terminator. Recovery must leave an outer construct's delimiter available to its
owner and must resume at the next valid statement or block delimiter without
silently discarding a complete sibling statement.

The parser must include `if`, `while`, `break`, and `continue` as statement
recovery points. Recovery must also recognize `elseif`, `else`, and `end` as
synchronization points while tracking the currently open construct. A malformed
nested block must not cause the parser to consume the closing `end` that
belongs to an outer block.

The lexer reserves `if`, `elseif`, `else`, `while`, `break`, and `continue`.
`end` is already reserved by RFC 0008. The new words cannot be declared as
values, types, members, parameters, or function names. The lexer must produce
distinct token kinds for all six new keywords; the parser must not recognize
them by raw identifier spelling.

### Conditions

For every `if`, `elseif`, and `while` condition:

1. check the expression in a Boolean-required context;
2. require the resolved type to be exactly `Bool`;
3. preserve the checked expression structure for generation; and
4. report the condition's earliest independent name, syntax, or type error.

The condition is not a declaration context and does not make a new binding.
Existing operator rules apply unchanged. In particular, `and` and `or` retain
their left-to-right short-circuit behavior.

The requirement is exact even after RFC 0014 introduces unions: `Bool | Nil`,
`Bool | Other`, and every other union type are not conditions merely because
one member is `Bool`. A future narrowing construct may produce an exact `Bool`
value before it is used as a condition; this RFC adds no implicit narrowing.

These examples are rejected:

```seawitch
if count                    // Type Error: expected Bool, got Int32
end

while pointer               // Type Error: expected Bool, got Ptr<Node>
end
```

### Execution semantics

An `if` statement evaluates its first condition once. If it is true, only the
first body executes. Otherwise the next `elseif` condition is evaluated once,
in source order, until one is true. If no condition is true, the `else` body
executes when present. At most one body executes, and no later condition is
evaluated after a selected branch.

A `while` statement evaluates its condition before each iteration. If it is
false initially, the body does not execute. If it is true, the body executes
once and the condition is evaluated again after the body completes normally or
after a `continue`. A `break` exits the loop immediately and continues with the
first statement after its closing `end`. `return` exits the enclosing function
or method and therefore exits any active control-flow blocks.

`break` and `continue` affect only the innermost enclosing `while`. They are
not valid in a conditional that is not nested in a loop, and they never target
a loop in a caller.

Each full statement executes in source order. Expression evaluation order
continues to follow RFC 0008 and RFC 0009. The control-flow construct itself
does not introduce a new left-to-right guarantee for unrelated expressions.

The checker checks loop bodies even when a condition is an immutable constant
`false`. There is no dead-code elimination or constant-condition diagnostic in
this RFC.

### Lexical scopes

Every branch body and every `while` body is a child lexical scope. The scope
starts immediately after the construct's condition or branch keyword and ends
at that body's delimiter:

```seawitch
mut total: Int32 = 0
if enabled
    mut local: Int32 = 1
    total = total + local
end

value: Int32 = local       // Type Error: local is out of scope
```

The condition of an `elseif` is outside the preceding branch body. A name
declared in one branch is therefore not visible in another branch or after the
conditional. A name declared in a loop body is not visible in its condition or
after the loop. A declaration inside a nested block may shadow a visible outer
value; duplicate declarations in the same lexical scope remain errors. Sibling
blocks may independently declare the same spelling.

Assignments and references may use visible bindings from enclosing scopes.
Mutation remains subject to RFC 0007: a fixed binding cannot be assigned, while
a mutable binding can be assigned when its target is writable. Block scope
does not change a binding's mutability or type.

The scope parent and visibility rules are:

1. A module-level block has the module scope as its parent. It can read and
   write only module bindings already visible at that source position, subject
   to the ordinary mutability and place rules. Its block bindings disappear at
   `end` and are not visible to later top-level items.
2. A control-flow block inside a function or method has the RFC 0008 function
   scope, or its enclosing control-flow frame, as its parent. It inherits
   parameters, locals, `self`, and the module-level functions or methods that
   RFC 0008 makes visible. It never gains access to module-level data bindings
   through a new block scope.
3. `self` remains an implicit fixed binding inside a method and cannot be
   declared or shadowed. Type names remain in the separate type namespace and
   cannot be declared inside a block or shadowed by a value binding.

Every value binding receives a compilation-scoped binding identity when it is
declared. A checked declaration records that identity, and every checked
variable, place, address, and dereference operation records the resolved
identity rather than only its source spelling. Scope lookup walks parent
frames; entering a branch or loop pushes a frame and leaving it pops that
frame. A failed declaration is never inserted into its frame.

The generator uses the binding identity for every reference and declaration.
Generated C value names must be deterministic and injective for all bindings
in one generated function. The RFC 0004 private spelling is the base name;
when two bindings would otherwise collide, the generator adds a deterministic
declaration-order suffix. This keeps nested shadowing and sibling reuse
readable without allowing one binding's reference to resolve to another.

### Runtime declaration lifetime

Lexical scope also determines when a declaration is initialized:

- a branch declaration is initialized only when that branch executes;
- a `while` body declaration is initialized on every iteration that reaches
  the declaration; and
- an assignment to an enclosing mutable binding remains visible after the
  block, while an assignment to a shadowing binding affects only that binding.

No destructor, move, or other scope-exit runtime behavior is introduced. The
generated C compound scopes represent the same declaration lifetime and name
visibility.

### Definite return and fallthrough

RFC 0008's rule that a returning function's final body statement must be
`return` is replaced by a conservative no-fallthrough rule. Define
`fallsThrough(statements)` as whether at least one normal execution path can
reach the end of the statement sequence:

- `fallsThrough([])` is true;
- `fallsThrough([return, ...])` is false, because later statements are
  unreachable on every path that reaches them;
- an ordinary declaration, assignment, call, or other non-control-flow
  statement falls through;
- an `if` falls through when it has no `else`, or when any branch body falls
  through; and
- a `while` always falls through because its condition may be false before the
  first iteration, or because its body may execute `break`, even when its body
  contains a `return`.

`break` and `continue` never constitute a function or method return. Inside a
loop body, `break` creates a path to the statement after that loop and
`continue` creates a path back to the loop condition. Neither can make the
enclosing function or method definitely return. A loop remains
non-definitely-returning even when every syntactic branch in its body ends in
`return`, `break`, or `continue`.

The flow summary for a loop body is interpreted relative to the nearest loop:
the nearest `while` consumes its body's `break` edges as exits to the statement
after that loop and its `continue` edges as back-edges to the loop condition.
A nested `while` consumes its own loop-control edges first; the enclosing loop
sees the nested `while` only through that statement's normal fallthrough
result. This rule prevents a `break` from being mistaken for a function return
or for a break from an outer loop.

For a sequence, inspect statements in order. A statement that does not fall
through prevents later statements from being reached; otherwise continue with
the next statement. A sequence is definitely returning exactly when its
`fallsThrough` result is false. An `if` is definitely returning when it has an
`else` and every `if`, `elseif`, and `else` body has `fallsThrough == false`.
An empty branch therefore prevents definite return. This definition also
accepts a returning conditional followed by no statement and accepts a normal
statement after a `return` without adding unreachable-code diagnostics.

Definite-return analysis runs on the fully checked statement tree. If a child
statement already has a syntax, name, type, or scope diagnostic, the checker
does not add a derived fallthrough diagnostic for that same body. This preserves
earliest diagnostic ownership and avoids reporting a missing return caused only
by an invalid child being omitted from checked output.

A returning function or method is rejected only when its checked body has
`fallsThrough == true`:

- a `return` statement definitely does not fall through;
- an `if` with no `else` can fall through;
- an all-branch-returning `if` cannot fall through; and
- a `while` never proves that execution cannot fall through.

Consequently, this is valid:

```seawitch
fun absolute(value: Int32): Int32
    if value < 0
        return -value
    else
        return value
    end
end
```

This remains invalid because the conditional has no `else` path:

```seawitch
fun positive_or_zero(value: Int32): Int32
    if value > 0
        return value
    end
end
```

A `while` cannot satisfy the return requirement even when its condition is the
constant `true`; the analysis does not prove non-termination. This rule does
not add unreachable-code diagnostics. It only prevents a value-returning
function or method from falling through without returning a value. A
fallthrough diagnostic points at the enclosing function or method's closing
`end`, matching RFC 0008's body-completeness diagnostics.

### Checked representation

The checker adds explicit generator-ready statement nodes:

```text
IfStatement {
    Condition Operand
    Then []Statement
    ElseIf []IfBranch
    Else []Statement       // nil when absent
}

IfBranch {
    Condition Operand
    Body []Statement
}

WhileStatement {
    Condition Operand
    Body []Statement
}

BreakStatement {
    SourceLine int
    SourceColumn int
}

ContinueStatement {
    SourceLine int
    SourceColumn int
}
```

The concrete representation may use equivalent Go types, but every condition
must retain its resolved `Bool` type and every body must contain only checked
statements. Source locations for the control-flow keyword and condition are
retained for diagnostics and `#line` generation.

Each checked declaration and each resolved reference also retains its binding
identity. A condition is checked in the enclosing scope before its body frame
is pushed; each branch body and loop body is checked with a fresh child frame.
The `elseif` condition is checked after the preceding branch frame is popped,
so it cannot see declarations from that branch.

The checker carries a loop-context depth while recursively checking bodies.
Entering a `while` increments the depth for its body and leaving it restores
the previous value. `break` and `continue` are accepted only when the depth is
nonzero; nested `if` blocks do not change it. The loop context is reset when a
function or method body begins, so a control-flow statement cannot target a
caller-owned loop.

The checker must recursively dispatch every supported statement in every block.
An invalid child statement is not silently dropped. A failed block statement
does not enter its parent scope, and an invalid declaration does not enter its
own block scope. Independent diagnostics may continue to be collected under
the existing fail-closed rules. The checker runs definite-return analysis only
when the body has no child diagnostics that would make the analysis
unreliable.

No analyzer pass is required. The checked control-flow tree may be passed
directly to generation under ADR 0001.

### C23 lowering

The generator emits explicit braces for every Seawitch block, including empty
blocks:

```seawitch
if ready
    work()
elseif retry
    retry_work()
else
end
while active
    tick()
end
```

```c
if (sw_v_ready) {
    sw_f_work();
} else if (sw_v_retry) {
    sw_f_retry_work();
} else {
}

while (sw_v_active) {
    sw_f_tick();
}
```

Loop control lowers directly to the C statements inside the generated `while`:

```c
while (sw_v_active) {
    if (sw_v_done) {
        break;
    }
    if (sw_v_skip) {
        continue;
    }
    sw_f_work();
}
```

The generator emits no labels or `goto` statements for these forms. C's
nearest-loop behavior matches the checked Seawitch loop context.

`elseif` lowers to C `else if`; it does not create an extra C nesting level
that could change declaration lifetime or source mapping. Each Seawitch branch
body nevertheless receives its own C compound scope. The C condition is
rendered from the checked `Bool` operand and is evaluated with the execution
frequency specified above. A `while` condition is rendered inside the C loop
header so it is reevaluated by the C runtime.

The generator must render nested statements recursively and preserve the
resolved binding identity of each declaration and reference. Its validation
state has a scope stack: entering a generated branch or loop pushes a frame,
and leaving it restores the enclosing frame. A declaration in one sibling
branch must not remain available while rendering another branch or the code
after the enclosing construct. The generator's deterministic binding-name
allocator must agree with the checked binding identities and must not emit
duplicate C names for distinct bindings in one generated function. Its loop
context must also track the nearest generated `while`; a checked `break` or
`continue` with no active loop is an impossible checked state.

Generated declarations, conditions, branch keywords, loop keywords, and
control-flow bodies receive `#line` directives derived from their Seawitch
source locations, consistent with RFC 0008. An `elseif` condition maps to its
own source line even though it lowers to a C `else if`.

The generator must fail with an `Unknown Error` if a checked conditional or
loop has a missing condition, a non-Boolean condition, an invalid child node,
or an otherwise impossible representation. It must never emit a placeholder
comment or silently omit a block.

## Diagnostics

The earliest phase that can prove a failure owns its diagnostic:

```text
[Syntax Error] expected a condition after 'if'
[Syntax Error] expected 'end' to close 'while'
[Syntax Error] 'else' must be the final clause of an if statement
[Syntax Error] unexpected 'elseif' outside an if statement
[Type Error] expected Bool in if condition; got Int32
[Type Error] local is out of scope
[Type Error] break is only valid inside a while loop
[Type Error] continue is only valid inside a while loop
[Type Error] returning function absolute may fall through without returning Int32
[Unknown Error] unsupported checked control-flow statement
```

Required structural diagnostics include:

- missing conditions after `if`, `elseif`, or `while`;
- a missing `end` for either construct;
- `elseif` or `else` outside an `if`;
- a second `else` clause;
- an `elseif` appearing after `else`;
- `else` or `elseif` appearing while a `while` body is open;
- an unexpected `end` at module level;
- a `type`, `fun`, or `impl` declaration inside a block;
- `break` or `continue` outside a `while` body; and
- `return` at module level.

The parser owns block structure errors. The checker owns condition type,
binding, scope, and definite-return errors. The generator owns only impossible
checked-state errors.

## Non-goals and rejected alternatives

### Truthiness

Rejected. Requiring `Bool` makes conditions statically obvious and avoids C's
implicit integer and pointer truthiness rules. Explicit comparisons remain
available:

```seawitch
while index < limit
    index = index + 1
end
```

### `else if` as a second spelling

Rejected. `elseif` gives one obvious conditional-chain form. `else if` remains
the ordinary nested-block spelling and requires two `end` keywords.

### Labeled loop control

Deferred. `break` and `continue` always target the innermost `while`. Labeled
loops and selecting an outer loop require a separate naming and resolution
design.

### Conditional expressions

Deferred. `if` is statement-only and produces no value. A future expression
form must define branch result typing and evaluation before being added.

### Implicit block scope

Rejected. Every branch and loop body has an explicit lexical lifetime matching
its `end`, which keeps declaration visibility and generated C lifetime
predictable.

## Implementation acceptance criteria

Implementation is complete when end-to-end tests prove that:

1. the lexer reserves and emits distinct tokens for `if`, `elseif`, `else`,
   `while`, `break`, and `continue`, while `end` continues to work for all
   block constructs;
2. single-branch conditionals, multiple `elseif` branches, optional `else`,
   `while`, `break`, and `continue` parse without terminators;
3. nested and adjacent control-flow blocks match the nearest `end` correctly;
4. missing `end`, misplaced clauses, duplicate `else`, `elseif` after `else`,
   `else`/`elseif` inside a `while`, and loop-control statements outside a
   `while` produce structured diagnostics;
5. conditions require exactly `Bool`, including conditions using comparison,
   `and`, `or`, parentheses, and rejected union-valued conditions;
6. selected `if` branches and `elseif` conditions obey the specified
   short-circuit execution order, `while` conditions are reevaluated,
   `break` exits the innermost loop, and `continue` reevaluates its loop
   condition;
7. declarations have branch or loop scope, sibling declarations may reuse a
   name, nested shadowing resolves to distinct binding identities, binding
   names are deterministic and collision-free in generated C, and outer
   mutable bindings remain assignable from a block;
8. `return` exits from nested blocks and definite-return checking accepts an
   all-branch-returning conditional, accepts a return followed by unreachable
   statements, rejects fallthrough, suppresses derived fallthrough errors when
   child diagnostics already exist, treats `while` as non-definite, and never
   treats `break` or `continue` as a function return;
9. module-level and function/method-level blocks lower to readable C23 with
   explicit braces, direct `else if`, direct `break` and `continue`, correct
   declaration initialization lifetime, correct declaration scope, and
   preserved `#line` mappings;
10. every new parsed, checked, and generated node is handled explicitly and
   fail-closed; and
11. integration coverage lives in a facet-named control-flow test file, while
   any real C23 compilation checks remain behind the `c23` build tag. Coverage
   includes empty blocks, zero-iteration loops, nested block recovery,
   function/module visibility, branch/loop scope cleanup, nested-loop targeting,
   and invalid loop-control placement.

## Implementation handoff requirements

The implementation plan must identify:

1. lexer token additions and parser block-recursion/recovery rules;
2. the parsed and checked conditional, loop, and loop-control representations;
3. nested scope lookup, declaration binding identity, scope-frame push/pop,
   shadowing, and deterministic generator name allocation;
4. condition checking, loop-context checking, and the definite-return
   algorithm for nested blocks;
5. parser synchronization around nested delimiters and recursive checker and
   generator dispatch with fail-closed unknown-node diagnostics;
6. focused lexer/parser/checker/generator tests and end-to-end control-flow
   tests; and
7. updates to `docs/grammar.md`, `docs/language.md`, and `docs/status.md` only
   after the implemented behavior stabilizes.

The feature remains forward-only and does not require an analyzer pass.
