# RFC 0254: Reference `self` and Explicit Mutating Methods

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. Implemented and verified 2026-09-30: `self` names the
  caller's storage in every method, `method mut` is the declared contract
  verified against each body, a `mut` call requires a writable receiver,
  every method lowers through a pointer receiver, the receiver-copy rule and
  its diagnostic are removed, and the snippet artifacts of the three
  method-declaring snippets moved; see Implementation state
- Supersedes: the current value-copy `self` and shallow-copyable receiver rules
  in `docs/reference.md`; archived RFC 0162 remains historical
- Depends on: RFC 0255 (stale collection views) for element receivers and the
  shared callable effect/borrow summaries
- Sequencing: RFC 0255 lands first. The first implementation supports the
  existing struct receivers only; a later union-receiver decision must extend
  the same explicit contract rather than changing this RFC's method table.

## Summary

`self` becomes a reference to the receiver's storage in every method. A method
declared with `method mut` may write its receiver's `mut` members; a method
without `mut` is readonly. A call to a `mut` method is rejected when its
receiver cannot take the write, which is the case where the write would land
on a copy and vanish.

```hexal
type Account is struct mut balance: Int64 end

method mut Account.deposit(n: Int64) do
    self.balance = self.balance + n          -- writes the caller's storage
end

method Account.balance_of(): Int64 do
    return self.balance
end

let mut acct = Account(balance = 0)
acct.deposit(50)                             -- acct.balance is 50
print(acct.balance_of())
```

## Motivation

RFC 0162 made `self` a fixed copy. A method can therefore only read its
receiver or return a modified copy, so every stateful type splits its API:

```hexal
print(acct.balance_of())      -- a method
deposit(@acct, 50)            -- mutation must be a function
acct = acct.deposited(50)     -- or copy, change, and copy back
```

It also rejects every method on a struct containing `Atomic<T>`, because the
receiver must be copied.

## Semantics

### `self`

- In every method, `self` names the receiver's storage. Reading `self.m`
  reads the caller's member; writing `self.m = v` writes it.
- Only `mut` members are writable through `self`. Non-`mut` members stay
  fixed after construction, and assigning to `self` itself stays rejected.
- `@self` yields `Ptr<mut T>`, and `@self.m` yields a pointer to the caller's
  member, subject to the existing `@` rules. The grammar and parser admit
  `self` as an address-place root; they do not treat it as an identifier.
- A result reached through `self` borrows the receiver. Returning that result
  from the method is valid, but calling such a method on a temporary or call
  result is rejected because the materialized receiver would die while the
  result remained usable. The checked method record carries `result borrows
  self`, and a call propagates the receiver's storage root to the result.
- Methods are not values and cannot be assigned. `self.name = ...` or
  `v.name = ...` naming a method stays rejected. A method cannot change
  another method.

### Explicit `mut` contract

`method mut T.name(...)` declares that the method may write receiver-owned
storage. A method without `mut` is readonly. Its body is rejected with
`type.readonly-method-writes-self` when any path:

1. assigns to receiver-owned storage: `self.m`, a value member below it, or an
   inline `List<T, N>` element below it;
2. takes a writable address rooted at `self` (`@self`, `@self.m`), because the
   pointer can carry the write anywhere;
3. calls a `mut` method on a receiver rooted at `self`
   (`self.deposit(1)`, `self.inner.reset()`).

A `mut` method may currently have a readonly body. The declaration is the
stable public contract, not a promise that this version of the body performs a
write. Removing or adding `mut` is an explicit interface change.

The walk stops at an indirection boundary. Writing through a `Ptr`, Slice,
allocated `List`, Dict, String, or another handle changes separately owned or
borrowed storage, not the receiver's bytes. Therefore both
`self.items.push(x)` and `self.items[0] = x` on an allocated `List<T>` member
are not writes to self. The same element assignment on an inline
`List<T, N>` member is a write to self.

Body verification uses resolved method identity and resolved calls, never the
method name alone. Generic templates and specializations are each checked
against the same declared `mut` contract after their type-dependent calls
resolve. Imported checked method records carry the declared `mut` bit and the
inferred `result borrows self` summary; importers trust those records and
diagnose invalid calls at the importing call site. Both facts participate in
the module's exported-interface fingerprint.

Deferred calls capture the receiver place at registration, consistently with
the existing defer capture rule. A deferred result-borrowing call keeps a
materialized receiver alive until the deferred invocation completes. An
address derived from `self` may not escape into a spawned Task; cross-Task
borrowing requires a future concurrency-safety design.

### Receivers

A call `r.m(args)` to a `mut` method is valid exactly when
`@r` would be a writable address. The existing place classification decides
this (`checkPlace(...).Writable`, `compiler/checker/places.go`):

| Receiver | `mut` method | Readonly method |
| --- | --- | --- |
| `let mut` binding, or a `mut` member path of one | ok | ok |
| `Ptr<mut T>` (one autoderef) | ok, writes the pointee | ok |
| `Ptr<mut T> | Nil`, after non-nil narrowing | ok, writes the pointee | ok |
| writable element place (`xs[i]` of a writable List) | ok, subject to RFC 0255 | ok |
| fixed `let` binding | rejected | ok |
| parameter | rejected | ok |
| `for` binder | rejected | ok |
| `Ptr<T>` | rejected | ok |
| temporary or call result | rejected | ok |
| non-`mut` member path | rejected | ok |

The rejection is `type.method-writes-fixed-receiver`:
`mut method <T>.<name> requires a writable receiver, but <receiver> is not writable`.
For a `for` binder, the note says: `write through the collection instead:
for i, x in xs do xs[i].<name>(...) end`.

A `mut` call is checked as if its receiver were an implicit first `@r`
argument. That implicit view participates in RFC 0255's complete call-argument
check; it is not merely a C-lowering detail. Existing writable-address rules
continue to apply:
- binding escape and narrowing invalidation (`type.cannot-narrow-after-address-escape`);
- freed-state checks;
- RFC 0255's view rules, when `r` is an element place: the method is called
  with a view of the collection as `self`, so passing that collection in the
  same call is rejected with `type.collection-view-passed-with-root`;
- the traversal rule. A method whose checked effect is limited to writing its
  element receiver is proven non-structural and may be called as
  `xs[i].m(...)` during traversal. Passing or capturing `xs`, or calling an
  operation that may structurally change it, remains rejected.

Assigning to `self` itself remains rejected. Hexal does not expose the
generated C pointer as a Hexal `Ptr`, so `^self = value` is not a second way to
replace the whole receiver.

A readonly call on a pointer receiver no longer copies the pointee. `self`
refers to it directly.

## C lowering

- Every method takes `self` by pointer: `T *self` for a `mut` method,
  `const T *self` otherwise. Member access in method bodies lowers through
  `self->`.
- A call on a place passes its address. A call on a temporary or call result is
  valid only for a readonly method whose result does not borrow self. It
  first materializes the value in a hoisted local, then passes that local's
  address, following the existing try-hoisting pattern.
- A call on `Ptr<T>` or `Ptr<mut T>` passes the pointer. The one-layer
  pointee copy that RFC 0162 required is removed.

## Swept code

The receiver copy existed only because `self` was a value. Removing the copy
removes:

- `methodReceiverCopyDiagnostic` and `type.method-receiver-not-copyable`
  (`compiler/diagnostics/checker_methods.go:33`), with their tests in
  `compiler/tests/integration/method_receiver_test.go`. Structs containing
  `Atomic<T>` may now declare methods.
- The receiver-copy branches in `adaptReceiver` while preserving its separate
  compiler-owned pointer-target adaptation. The generator already renders the
  checked receiver expression directly; there is no generator-owned pointee
  copy to remove.
- The `docs/reference.md` text saying that a method copies `self` on entry,
  that writing `self.field` is rejected, and that mutation needs an explicit
  function taking `Ptr<mut T>` instead of a method.

## Documentation

`docs/reference.md`, functions and methods section:

- Replace the `self` copy bullets with the reference-`self` and explicit `mut`
  rules
  above, the receiver table, and the new diagnostic.
- Remove the shallow-copyable receiver requirement.
- State that adding or removing method `mut` is an explicit exported-interface
  change; editing a body within its declared contract is not.

`GRAMMAR.ebnf` and the parser: add optional `mut` between `method` and the
receiver type, and admit `self` as the root of an address place for `@self`
and `@self.member`.

## Alternatives considered

- **Infer mutation from the method body.** This avoids one token but makes a
  body-only edit change the exported call contract and reports fallout at
  callers. Rejected in favor of reusing the existing `mut` word explicitly.
- **Every method writable, all calls need a writable receiver.** This
  rejects read-only calls on parameters, `for` binders, and fixed bindings.
  Rejected.
- **Parameters and `for` binders as references.** This makes every struct
  assignment an alias and abandons value semantics. It is out of scope.

## Validation

Integration tests in `compiler/tests/integration/functions_test.go` unless
noted:

1. A `method mut` assigning `self.m` on a `mut` member compiles. The generated
   C declares `T *self` and writes `self->hex_m_m`.
2. A readonly method that only reads compiles with `const T *self`.
3. Assigning a non-`mut` member through `self` is rejected with the existing
   read-only-member diagnostic.
4. Assigning `self` itself is rejected with the existing fixed-binding
   diagnostic.
5. A readonly method `a` calling later-declared `mut` method `b` on self is
   rejected at `a` with `type.readonly-method-writes-self`; declaring `a` as
   `mut` accepts the same forward call.
6. Mutually recursive `mut` methods compile. A readonly member of the cycle
   that calls a `mut` member on self is rejected at its declaration.
7. `@self` passed to a `Ptr<mut T>` parameter compiles inside `method mut` and
   is rejected inside a readonly method.
8. `self.items.push(x)` on a `List<T>` member compiles in a readonly method and
   calling that method on a fixed binding compiles.
9. A `mut` call is rejected on each fixed receiver kind in the receiver
   table: fixed binding, parameter, `for` binder, `Ptr<T>`, temporary, call
   result, non-`mut` member. The same method called on `let mut` and
   `Ptr<mut T>` compiles.
10. A readonly method called on a temporary compiles, and the generated C
    hoists the temporary before passing its address.
11. An imported `mut` method called on a fixed binding in the
    importing module, is rejected.
12. A generic `mut` method keeps that contract for every specialization. A
    readonly generic specialization that resolves a call to a `mut` method on
    self is rejected at specialization.
13. A struct containing `Atomic<T>` declares and calls a method
    (`compiler/tests/integration/method_receiver_test.go`, replacing the
    shallow-copyable rejections).
14. A narrowing on `acct.m` is cleared after calling `mut` method
    `acct.deposit(1)`.
15. `xs[0].absorb(xs)`, where `absorb` is `mut`, is rejected with
    `type.collection-view-passed-with-root` (RFC 0255).

C23 lane (`compiler/tests/c23validation/`):

16. `let mut acct`, `acct.deposit(50)` twice, then `print(acct.balance_of())`,
    prints `100`.
17. `for i, a in accounts do accounts[i].deposit(5) end` updates every
    element.

Additional integration cases:

18. `@self` and `@self.member` parse as address places in `method mut`; both
    are rejected in a readonly method. Assigning `self` itself remains
    rejected.
19. A method returning a pointer or Slice reached through self works on an
    addressable receiver; the same call on a temporary or call result is
    rejected before generation.
20. Pointer dereference, Slice element, allocated-List element, and handle
    operations below self compile in a readonly method; an inline-List element
    assignment requires `method mut`.
21. Two receiver types with the same method name retain their independent
    declared `mut` contracts.
22. A type-dependent generic specialization verifies its body against the
    template's declared `mut` contract.
23. An imported `mut` method succeeds through `Ptr<mut T>` and emits
    matching `T *self` declarations in producer and consumer artifacts. An
    imported readonly method emits matching `const T *self` declarations.
24. A deferred `mut` method mutates the receiver captured at registration.
    An address derived from self cannot escape through `spawn`.
25. Adding/removing declared `mut`, or changing the inferred `result borrows
    self` summary, changes the exported-interface fingerprint and invalidates
    dependants. A body-only edit within the declared contract does not.

The snippet manifest moves for every catalog snippet that declares a
method, because method C signatures change. The commit message lists
the moved artifacts.

## Implementation plan

1. Update `GRAMMAR.ebnf` and parser place handling for `self`-rooted address
   places; add focused parser cases before changing checker semantics.
2. Parse `method mut Receiver.name`, then extend checked and exported method
   records with the declared `mut` bit and inferred `result borrows self`;
   include both facts in interface fingerprints.
3. Replace receiver-copy validation with reference-self checking. Remove all
   three `methodReceiverCopyDiagnostic` call sites in ordinary, generic
   declaration, and generic specialization checking; retain unrelated pointer
   target adaptation.
4. Verify each ordinary and specialized body against its declared contract.
   Classify receiver-owned writes at representation boundaries and reject a
   readonly method at its first proven write, writable address, or `mut` call
   on self.
5. Propagate receiver storage provenance to results marked `result borrows
   self`; reject temporary receivers and Task escapes where the borrow would
   outlive the receiver.
6. Treat a writing receiver as an implicit first address argument for address
   escape, narrowing, freed-state, traversal, and RFC 0255 call checks.
7. Lower every method through a pointer receiver, preserve `const` for readonly
   methods, remove receiver copies, and add textual assertions for
   declarations, calls, and temporary hoists.
8. Add diagnostic registry records and typed emitters for every new key, then
   complete the exhaustive integration and C23 validation above.
9. Synchronize `docs/reference.md`, regenerate only legitimately changed
   snippet hashes, review the artifact-family diff, and run `gofmt -l`,
   `go test ./...`, `go vet ./...`, and the focused tagged C23 lane.

## Implementation state

- The declared `mut` bit is carried by the parsed, checked, specialized, and
  exported method records and by the call node (`MutatingMethod`), so a
  dependent module sees the contract through the registry record it already
  reads. The tree has no interface-fingerprint mechanism, so validation 25's
  fingerprint and dependant invalidation have nothing to attach to; the
  compiler recompiles every module on each call.
- User-method receivers adapt through `adaptMethodReceiver` to a pointer:
  `&place` (typed `Ptr<mut T>` or `Ptr<T>` by the place), the pointer value,
  or the address of a materialized temporary. `adaptReceiver` remains only for
  compiler-owned pointer-target operations.
- Readonly verification is in-line at the first proven write, at the three
  sites the spec names plus inline-List mutators and `mut_slice`, because the
  contract is declared, not inferred. A readonly generic template that writes
  a receiver member independent of its type parameters is rejected at the
  template; a write that resolves only per specialization is rejected at the
  specialization.
- A temporary receiver is lowered as a one-element `const` compound literal
  (`(const T[1]){ value }`) instead of a hoisted local: hoisting would evaluate
  the receiver before operands written earlier and under short-circuit
  operators, while the compound literal evaluates it where it is written and
  still gives the call an address. The evaluation-order pass already hoists it
  ahead of later effects.
- `result borrows self` is the receiver entry of the stale-view summary. An
  address derived from `self` is an abstract view of the caller's storage, so
  a method returning one records the borrow and a call on a temporary
  receiver is rejected by the view pass; the two diagnostics this adds are
  `type.method-result-borrows-temporary` and `type.self-address-escapes-task`.
- A readonly method on a struct that contains an `Atomic` keeps a non-const
  receiver pointer, because atomic operations need a non-const object.
- The swept receiver-copy rule, its diagnostic, the three call sites, and the
  value-copy lowering tests are removed; the shallow-copyable rejections
  became an Atomic-struct acceptance test.
- The snippet manifest moved exactly the artifacts of
  `functions-generic-container`, `functions-receiver-forms`, and
  `modules-nested-import` (`modules/app.c` three times, `modules/app.h`,
  `modules/graphics/shapes.c`, `modules/graphics/shapes.h`): the method C
  signatures and calls, and nothing else. The snippet sources needed no edit.

## Settled decisions

- Receiver mutation is explicit with `method mut`; body analysis verifies the
  declaration rather than inferring its public contract.
- The first implementation supports existing struct receivers only. Union
  receivers remain with RFC 0251 and must adopt the same explicit `mut`
  contract if introduced.
