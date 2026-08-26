# RFC 0116: Native Module Storage and Linkage

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-22
- Scope: Hexal-defined module storage, symbol visibility, and native linkage
- Depends on: RFC 0034 (modules and imports), RFC 0035 (copying and manual
  lifetimes), RFC 0052 (target profiles), the implemented function-value
  surface (`docs/reference.md`), and RFC 0110 (affine ownership)
- Coordinates with: RFC 0039 (C interoperability), RFC 0117 (compile-time
  evaluation), RFC 0118 (concurrency safety), the generated-C naming rules,
  `docs/reference.md`, and `docs/status.md`

## Summary

Add explicit module-level storage without adding implicit constructors,
initialization order rules, or a hidden runtime state object. The initial
surface has three declarations:

```hexal
const page_size: Size := 4096
static cache_count: Size := 0
export static device_status: UInt32 := 0
```

- `const` is immutable compile-time data. It may be inlined and need not have
  an address; taking its address gives it one read-only definition.
- `static` is one mutable or immutable object with module-private linkage.
- `export static` is one object with externally visible Hexal linkage.

All three require a compile-time initializer. Runtime setup remains an
ordinary function called by the program or host. Hexal does not acquire hidden
module constructors or unspecified cross-module initialization order.

The declarations cover lookup tables, device state, process-wide state,
embedded constants, and library symbols while preserving a small storage
model. C declarations for foreign storage remain owned by RFC 0039.

## Declaration rules

- Module storage declarations occur only at module scope.
- A declaration has one complete, finite type and one initializer.
- `const` initializers are constant expressions under RFC 0117 and produce an
  immutable value.
- `static` and `export static` initializers are constant expressions under RFC
  0117 and produce ordinary storage. Zero initialization is permitted when an
  explicit zero initializer is representable; an omitted initializer is not a
  new implicit initialization form.
- An affine owner cannot be module storage unless its type defines a static,
  constant representation with no runtime ownership action. Module storage
  never registers an implicit `defer` or destructor.
- A module storage name is visible only after its declaration in the same
  module. Cross-module access uses the existing import/name-resolution rules.
- A module storage declaration is not a function-local binding and cannot be
  shadowed by a declaration that changes the meaning of an existing qualified
  name.

## Mutability and access

- `const` and immutable `static` values are readable through ordinary value
  access and may be borrowed read-only.
- Mutable `static` values require an addressable mutable access path. A mutable
  access is exclusive under RFC 0110 and shared mutation is governed by RFC
  0118.
- Taking a reference to module storage does not transfer ownership. The
  storage exists for the entire program lifetime.
- A module storage object cannot be freed, moved, reset, or destroyed by user
  code. An owning type may still expose an explicit operation that changes its
  contents, subject to its type contract.
- `static` does not mean thread-local. Thread-local storage is a separate
  target/linkage capability and is not introduced by this RFC.

## Linkage and symbols

- Hexal module-private storage lowers to a C definition with internal linkage.
- `export static` lowers to one external definition whose generated name is
  stable under the module identity and declaration name.
- The generated C name is not the public ABI name. RFC 0039 owns explicit C
  symbol names, visibility attributes, packing, and foreign declarations.
- An exported object is emitted exactly once even when its module is reached
  through multiple imports.
- Two exported declarations with the same selected external name are an ABI
  diagnostic before C emission.
- A private storage declaration may be removed by the generator only when its
  address is not observable and its value has no required volatile or foreign
  semantics. `export static` is always addressable and externally observable.
- `volatile` access is an access qualifier, not a synchronization mechanism;
  its target and ordering rules remain those of the volatile operations in the
  reference and RFC 0118.

## C23 lowering

- `const` lowers to a C `static const` definition when storage is required and
  to an expression or initializer when it is not.
- Private `static` lowers to a generated C `static` definition.
- `export static` lowers to one generated external definition in the owning
  module translation unit and a declaration in generated headers when another
  generated translation unit needs it.
- Initializers contain only C constant expressions accepted by the selected
  target profile. The generator does not emit a hidden startup function for
  this RFC.
- The driver and linker, not the core compiler, determine whether an exported
  symbol is retained, visible from a shared library, or stripped by platform
  policy. RFC 0055 owns those build controls.
- C ABI storage, foreign globals, custom section placement, alignment
  attributes, linker sections, weak symbols, and assembler names require the
  explicit unsafe/FFI path of RFC 0039 unless a later target-profile RFC
  standardizes them.

## Concurrency

Every mutable module object is shared process state by default. Safe code may
not perform unsynchronized conflicting access. Atomic wrappers, Mutex, and
Channel operations use RFC 0118; a raw mutable module object is not implicitly
atomic merely because it has static storage.

## Non-goals

- Hidden module constructors or destructors.
- Dynamic initialization order across modules.
- Thread-local storage, linker sections, weak symbols, or custom C
  attributes in the safe core.
- Foreign globals or arbitrary C declarations; RFC 0039 owns those.
- A second module-level binding syntax that duplicates local `:=` inference.

## Validation

This section is exhaustive. RFC 0116 is complete only when every item below
passes:

- `const`, `static`, and `export static` are accepted only at module scope and
  use the specified initializer rules.
- A non-constant initializer is rejected with a compile-time diagnostic and no
  hidden initialization function is emitted.
- Private, exported, and imported storage receive the specified visibility and
  exactly-one-definition behavior.
- Taking a reference to module storage does not create an owner or permit
  deallocation.
- Mutable module storage participates in affine access checking and rejects a
  locally decidable unsynchronized data race under RFC 0118.
- An affine owner cannot be installed as module storage without an explicit
  type contract permitting static storage.
- Generated C text contains the expected definition, declaration, linkage, and
  initializer placement and contains no duplicate definition.
- Foreign ABI attributes and foreign globals are rejected here with guidance to
  RFC 0039 rather than silently receiving Hexal linkage.
- Ordinary tests remain pure Go and assert diagnostics and generated-C text;
  toolchain and linker behavior is owned by RFC 0055.

## Open questions

1. Whether the source spelling for external C symbol names is an RFC 0039
   attribute or a declaration modifier.
2. Whether a later RFC should add an explicit `thread static` declaration.
3. Whether immutable non-constant storage is worth adding after the constant
   initializer rule has shipped.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` with the exact
module-storage grammar, scope and visibility rules, initializer restrictions,
address/lifetime rules, and generated-C linkage contracts. Add module storage
to the module and declaration sections and remove the current statement that
Hexal has no native globals or static storage.
