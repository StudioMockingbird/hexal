# RFC 0093: Declaration and Assignment Operators

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-19
- Updated: 2026-08-19
- Scope: use `:=` for value/binding declarations and `=` for assignment
- Depends on: RFC 0090 (inferred binding declarations), `docs/reference.md`
- Implementation prerequisite: RFC 0092's completed snippet expansion is
  committed, and its 129-entry manifest is the fixed pre-0093 baseline
- Supersedes on implementation: RFC 0090's declaration-operator spelling; its
  type-determination rule is retained unchanged
- Coordinates with: the lexer, parser, checker, generator, integration tests,
  workbench snippets, the generated-artifact manifest, and `docs/status.md`
- Does not change: type declarations, module imports, object and ADT literal
  initializers, mutability rules, type identity, assignment targets,
  contextual-type restrictions, or generated C representation

## Summary

Hexal currently uses `=` both to initialize a value binding and to reassign an
existing mutable binding. This gives one token two distinct value-binding roles
and prevents the language from making declaration-versus-assignment errors
clear at the operator.

The language will use distinct declaration and assignment operators:

```hexal
count: Int32 := 13
mut total := compute()
total = total + count
```

`:=` introduces a value binding. In a value-binding context, `=` assigns to an
existing mutable place. Other grammar-defined uses of `=` remain unchanged.
A declaration has one governing type source. When an annotation is present,
that annotation is the destination type. Without an annotation, the
initializer must determine the binding type without destination context. This
RFC changes the value-binding operator, not the inference contract or unrelated
grammar.

## Normative rules

### Declaration

1. A declaration has the form `name: T := initializer` or
   `name := initializer`.
2. `:=` creates a new binding and is never a reassignment operator.
3. `mut` keeps its current meaning and may precede either declaration form:
   `mut name: T := initializer` or `mut name := initializer`.
4. Every declaration has an initializer. Uninitialized declarations are not
   introduced by this RFC.
5. A typed declaration is valid when the left side supplies `T`, even when the
   initializer is a contextual literal:

   ```hexal
   count: Int32 := 13
   label: String := "hexal"
   ```

6. An inferred declaration is valid only when the initializer determines its
   type without an expected destination type. Valid examples include:

   ```hexal
   total := compute()
   values := List<Int32>.new(h)
   entry := Entry { x = 1, y = 2 }
   copy := existing
   first := cursor.next()
   ```

   The type may come from a declared function result, explicit generic
   arguments, a named object literal, or an already typed binding.
7. An inferred declaration is rejected when neither side determines the type.
   This includes a bare integer, float, or string literal, `nil`, an array
   literal, a context-dependent match, and expressions whose type can only be
   selected from an expected destination type. Literal defaulting does not
   make such a declaration valid.
8. A value-binding declaration using `=` is invalid. In particular,
   `x: Int32 = 13` must produce a declaration-operator diagnostic rather than
   being accepted as an initialized declaration.

The reference grammar becomes:

```ebnf
declaration = typed-declaration | inferred-declaration ;
typed-declaration = [ "mut" ] , identifier , ":" , type-expression
                    , ":=" , expression ;
inferred-declaration = [ "mut" ] , identifier , ":=" , expression ;
```

### Other uses of `=`

This RFC does not repurpose `=` outside value-binding declarations and
assignments. The following existing forms remain valid and retain their current
meaning:

```hexal
type Entry = { x: Int32 }
module Math = import "./math"
entry := Entry { x = 1 }
shape := Shape.Circle { radius = 2 }
```

Type definitions, module aliases, object literal member initializers, ADT
payload initializers, and any other grammar-defined non-binding use of `=` are
not assignments and do not become `:=` forms.

### Assignment

1. `=` assigns to an existing place. The target must be declared, in scope,
   and writable under the existing mutability and pointer rules.
2. A bare assignment such as `x = 13` is invalid when `x` has not already
   been declared.
3. Assignment does not introduce a binding, infer a type, or alter the
   binding's declared type.
4. Existing assignment targets retain their current semantics, including
   mutable bindings, mutable object members, indexed collection elements, and
   writable pointer places.
5. `:=` rejects a declaration whose name already exists in the current scope.
   It may shadow an accessible outer binding exactly where the existing
   shadowing rules permit. It may not shadow a protected name, parameter,
   `self`, `for` binder, or import alias where the existing rules forbid it.

### Type determination

The type-determination contract in `docs/reference.md` remains authoritative
for the inferred form. RFC 0090 is immutable historical context, not normative
authority. The only semantic change is that the inferred form remains a
declaration while the explicitly typed form now uses the same declaration
operator:

| Form | Meaning | Valid when |
|---|---|---|
| `x: T := value` | typed declaration | `value` is assignable to `T` |
| `x := value` | inferred declaration | `value` determines a type without context |
| `x = value` | assignment | `x` is an existing writable place |

## Diagnostics

Diagnostics must be owned by the earliest phase that can prove the error:

- `x: T = value` reports a Syntax Error at `=` with the exact message
  `binding declarations require ':='; '=' assigns to an existing place`.
- `mut x = value` reports the same Syntax Error at `=`. `mut` introduces a
  mutable declaration; it never prefixes assignment.
- `x = value` without a prior binding retains the existing `unknown variable
  x` diagnostic at `x`.
- `x := value` with no type-determining side reports the existing inferred-
  declaration type diagnostic at `:=`.
- `x := value` or `x: T := value` where `x` is already declared in the current
  scope reports `variable x is already declared in this scope; use '=' for
  reassignment`; it must not be interpreted as assignment. A declaration in a
  permitted inner scope remains shadowing, not redeclaration.
- Assignment to a fixed binding or read-only place retains its existing
  mutability diagnostic.

## Migration

Implementation must:

1. Keep `:=` lexed as one adjacent token, including rejection of `: =` as a
   declaration spelling.
2. Preserve `:` between a binding name and its type, and replace the `=` after
   the completed type expression with the single `:=` token. Do not change
   type-definition, module-import, object-literal, or ADT-literal parsing that
   already uses `=`.
3. Preserve the current AST distinction between declaration and assignment;
   do not lower assignment into a declaration with a special name case. Rename
   `parser.Declaration.Infer` to `Operator`, store the `:=` token for both typed
   and inferred declarations, and use `Type == nil` as the sole inference
   predicate. Remove the old `Type == nil`/`Infer set` equivalence.
4. Rewrite every existing value-binding declaration in compiler test source
   strings, workbench snippets, `docs/reference.md`, `AGENTS.md`, and open or
   partially implemented specs from `name: T = value` to `name: T := value`.
   Leave actual reassignments and non-binding `=` forms unchanged. Do not edit
   closed or archived specs, including closed specs that remain outside the
   archive directory. Revise `AGENTS.md`'s closed-spec guidance separately from
   that spelling migration: pre-0093 specs may show the retired typed-binding
   form `name: T = value`; RFC 0090's narrow inference rule remains historical,
   while current typed and inferred declarations both use `:=`.
5. Add inferred declaration examples to the workbench catalog so the language
   surface includes both typed and inferred `:=` forms.
6. Retain RFC 0090 as an immutable historical record. Its catalog-rewrite
   non-goal is superseded by this RFC; its type-determination and rejection
   rules are not.
7. Remove RFC 0090 provenance from maintained code and test comments rewritten
   by this migration. Those comments must state the present declaration
   contract under CARE, cite no RFC or spec number, and contain only ASCII.
   This is not a repository-wide comment cleanup; unrelated comment violations
   remain outside this RFC.
8. Update parser recovery text that distinguishes statements to mention `:`,
   `:=`, and `=`: `expected ':' or ':=' for a declaration, or '=' for an
   assignment`.
9. Record the committed post-RFC0092 snippet manifest immediately before RFC
   0093 implementation as the baseline. Do not begin while that catalog change
   or another overlapping workbench/compiler change is uncommitted; existing
   category, snippet, artifact, and hash entries must be compared against that
   fixed baseline.

## Non-goals

- Inference for parameters, function results, object members, or ADT payloads.
- Uninitialized declarations or a new `var`/`let` form.
- Changes to mutability, shadowing, assignment targets, or type compatibility.
- Changing the generated C for an equivalent source program.
- Runtime execution of generated C as part of ordinary tests.

## Validation

This section is exhaustive. RFC 0093 is complete only when every item below
passes:

- `x: Int32 := 13` compiles and the value-binding form `x: Int32 = 13` is
  rejected with the exact declaration-operator diagnostic at `=`.
- `mut x = 13` is rejected at `=` with the same exact declaration-operator
  diagnostic; it is not parsed as assignment or given the old generic `mut`
  diagnostic.
- `x := compute()`, `values := List<Int32>.new(h)`,
  `entry := Entry { x = 1 }`, `copy := existing`, and a mutable inferred
  declaration compile when their initializers determine the type.
- Bare integer, float, and string literals; negated numeric literals;
  arithmetic containing only contextual numeric operands; `nil`; array
  literals; and context-dependent `match` inferred declarations are rejected
  with the inferred-declaration diagnostic.
- `flag := true`, `done := eos`, `byte := b'A'`, and `rune := 'A'` compile as
  Bool, EoS, UInt8, and Rune respectively; the implementation must not reject
  every literal indiscriminately.
- `x := cleanup()` is rejected with `cleanup produces no value` when `cleanup`
  has no result.
- `mut x: Int32 := 13` and `mut x := compute()` compile; reassignment with
  `x = value` works only for mutable bindings.
- Assignment to an undeclared or fixed binding is rejected. Same-scope typed
  and inferred redeclarations produce the exact redeclaration diagnostic;
  declaration in a permitted inner scope still shadows an outer binding.
- `x : = value` and `x: T : = value` remain syntax errors and are not accepted
  as `:=`.
- Parser unit coverage verifies that `Declaration.Operator` holds `:=` for
  both typed and inferred declarations, while only the inferred form has
  `Type == nil`.
- Type definitions, module aliases, object literal member initializers, and ADT
  payload initializers using `=` continue to compile unchanged.
- Every existing value-binding declaration in active compiler tests,
  documentation, and the workbench catalog uses `:=`; every existing
  reassignment and non-binding `=` form remains `=`.
- Maintained compiler and test comments contain no RFC 0090 provenance, and
  every comment rewritten by this RFC is ASCII-only; the statement-recovery
  diagnostic names `:`, `:=`, and `=` correctly.
- At least one catalog snippet demonstrates typed `:=`, one demonstrates
  inferred `:=` from a function result, and one demonstrates inferred `:=`
  from an explicitly parameterized collection.
- Representative typed and inferred equivalents produce equal complete
  `CompilationResult.Files` maps, including every selected component artifact,
  not only `modules/app.c`, `modules/app.h`, and `hexal.h`.
- No existing entry in the snippet manifest changes hash. The manifest gains
  entries only for genuinely new snippets; adding no new snippet means the
  manifest is byte-identical. Any permitted new entries are generated from
  compiler output rather than edited by hand.
- The manifest comparison uses the committed post-RFC0092 129-entry baseline;
  RFC 0092 and RFC 0093 changes are not combined in one uncommitted diff.
- `AGENTS.md` warns that pre-0093 closed specs may show retired typed
  declarations using `=`, and describes RFC 0090 as historical rather than as
  current operator authority.
- `docs/reference.md` is updated to make this operator split normative and
  RFC 0090's old declaration spelling is not left as active syntax.
- `go test ./compiler/...`, `go test ./workbench/snippets`, and `go test ./...`
  pass without an external toolchain.

## Handoff

After Validation passes, rebuild the workbench binary into `bin/` and restart
the running workbench before handoff, as required by `AGENTS.md`. This is an
execution step, not a testable language acceptance condition.
