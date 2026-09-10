# RFC 0161: Pointer Mutability and Plain Slices

- Kind: Language Semantics
- Status: Closed; implemented 2026-09-10. Lexer/parser/checker/generator implement
  `@` address-taking, prefix `^` dereference, `Ptr<mut T>` / `Slice<mut T>` spelling,
  `mut_slice` construction, outermost-only weakening, and first-class copyable Slices
  with no lifetime analysis; `docs/reference.md` synchronized in the same change.
  Full suite (`go test ./...`, `go vet ./...`, `gofmt`) green with the snippet
  manifest rebaselined to the renamed artifacts.
- Created: 2026-09-10
- Consolidates: the non-ownership syntax work identified in RFCs 0153 and 0154
- Supersedes on implementation: RFC 0153 and RFC 0154
- Does not depend on: RFC 0110, RFC 0149, or RFC 0155
- Invalidates: RFC 0149's Ref and Box designs, and the
  Ref/Slice-lifetime portions of RFC 0110. Those RFCs must not be implemented
  as written; any independently retained affine or unsafe work requires a
  later rescope
- Does not update `docs/reference.md`: synchronize only after implementation
  stabilizes and the user explicitly approves the reference edit

## Summary

Adopt one uniform spelling for writable pointer-like access and rename View to
a plain Odin-style Slice:

```text
MutPtr<T> -> Ptr<mut T>
View<T>   -> Slice<T>
             Slice<mut T> adds writable element access
Ref<T>    -> Ptr<T>
MutRef<T> -> Ptr<mut T>
Box<T>    -> explicitly allocated Ptr<mut T>
```

A Slice is an ordinary, copyable pointer-and-length descriptor:

```text
slice    = pointer + length
lifetime = programmer responsibility
bounds   = runtime checked
```

Slice is first-class. It may be assigned, stored, passed, returned, nested in
aggregates and collections, and copied. Copies alias the same elements while
holding independent pointer-and-length descriptors.

Hexal has no Ref or Box type. Ptr expresses access but not ownership or
provenance. Heap, Stash, and Pool allocate values and return Ptr<mut T>;
programs release or invalidate those allocations explicitly through the
allocator that owns them. Hexal adds no borrow block, scoped capability,
affine ownership, implicit move, destructor, automatic cleanup, lifetime
parameter, root tracking, or runtime borrow metadata.

## Goals

- Replace the separate `MutPtr` name with one Ptr family.
- Replace View with the conventional Slice name and add a writable mode.
- Use Ptr for both non-owning references and explicitly allocated objects;
  remove dedicated Ref and Box type concepts.
- Preserve zero-copy slicing, iteration, text-byte access, and IO composition.
- Keep Slice representation and semantics close to Odin and C.
- Check every index and sub-slice bound at runtime.
- Remove compiler-owned lifetime/provenance analysis from Slice.
- Preserve readable, direct C23 lowering.

## Accepted consequence: lifetime is manual

Slice does not own or retain its storage. The programmer must ensure that the
backing Array, List, String, allocation, or foreign region remains alive and is
not reallocated for every Slice use.

```hexal
fun bad(): Slice<Int32> do
    values: Array<Int32, 2> := [1, 2]
    return values.slice(0, 2)
end
```

The compiler accepts this program. Using the returned Slice after `values`
ceases to exist accesses invalid storage. Bounds checking cannot detect that:
the stale descriptor still contains a plausible pointer and length.

Likewise, growing or freeing a List invalidates Slices into its old storage;
freeing a String invalidates its byte Slices; resetting/destroying an allocator
invalidates Slices into its allocations; and concurrent source invalidation can
race with Slice access.

Such use is outside the Slice contract and may produce undefined behavior in
generated C. Slice therefore has the same manual-validity boundary as raw Ptr.
This is an explicit narrowing of Hexal's general no-undefined-behavior and "if
it compiles, it runs" goals, chosen to avoid a language-wide lifetime system.

## Non-goals

- Dedicated Ref or Box types, borrow blocks, lifetimes, affine types, move
  tracking, destructors, or automatic cleanup.
- Static dangling-Slice, alias, escape, or concurrent-invalidation prevention.
- Runtime owner pointers, generations, versions, reference counts, or borrow
  guards.
- Proving that two writable ranges are disjoint.
- Runtime-sized Array types or dependent return types.
- Pointer arithmetic, one-past pointers, unchecked indexing, or new casts.
- Changing existing allocation and explicit-cleanup contracts.

## Grammar

```ebnf
ptr-type = "Ptr" , "<" , [ "mut" ] , type-expression , ">" ;
slice-type = "Slice" , "<" , [ "mut" ] , type-expression , ">" ;
address-expression = "@" , unary-expression ;
dereference-expression = "^" , unary-expression ;
```

- Ptr and Slice each take exactly one complete valid type argument.
- `mut` is valid immediately before Ptr's or Slice's type argument only.
- `@` is the sole address-taking operator. It is a prefix operator whose
  operand must be one addressable place expression.
- Prefix `^` is the sole explicit dereference operator. Its operand must
  produce a concrete non-null Ptr. Prefix `^` and binary XOR use the same token
  and are distinguished by expression position, as unary negation and binary
  subtraction already are.
- `.value` ceases to have special pointer-dereference meaning. It remains an
  ordinary member name.
- `MutPtr`, `View`, and `MutView` cease to be type names and are not aliases.
- `Ref`, `MutRef`, `Box`, and `MutSlice` are not introduced and are not type
  aliases. These spellings are not reserved; a program may declare ordinary
  user-defined types with those names.
- `ref` ceases to be a keyword and retains ordinary identifier behavior.
- `borrow` is not introduced or reserved.
- `mut` inside Array, List, Dict, Fun, Task, Channel, Atomic, Stash, Pool, or
  any other generic constructor remains invalid.

## Pointer semantics

```text
Ptr<T>       read-only raw pointer to T
Ptr<mut T>   writable raw pointer to T
```

- Ptr records access permission only. It does not record whether its pointee is
  on the stack, heap, in an allocator, or in foreign storage; it does not own,
  retain, or automatically release the pointee.
- `Ptr<T>` lowers to `const T *` where C declarator placement requires it.
- `Ptr<mut T>` lowers to `T *`.
- `@place` produces `Ptr<mut T>` for a writable place and `Ptr<T>` for a fixed
  place. It lowers directly to C address-taking with `&`.
- `^pointer` produces the pointed-to T as a place. It is writable exactly when
  the operand is `Ptr<mut T>` and read-only when the operand is `Ptr<T>`.
- `^` is right-associative through ordinary unary-expression nesting:
  `^^pointer_to_pointer` dereferences two pointer layers.
- `@^pointer` is valid when `^pointer` is an addressable place and preserves
  its access mode.
- Pointer member access continues to auto-dereference one object-pointer layer.
  After `.value` loses its special meaning, `pointer.value` accesses an actual
  member named `value`; explicit whole-pointee access is `^pointer`.
- `Ptr<mut T>` weakens implicitly to `Ptr<T>` at the outermost layer only.
- Read-only-to-writable upgrade and nested weakening remain invalid.
- Nesting is structural: old `Ptr<MutPtr<T>>` becomes `Ptr<Ptr<mut T>>` and
  remains distinct from `Ptr<mut Ptr<T>>`.
- Existing pointer identity, eligibility, pointee restrictions, nullability,
  volatile operations, methods, C interoperability, lifetime contract, and C
  representation otherwise remain unchanged.
- `Heap.allocate<T>`, `Stash<T>.allocate`, and `Pool<T>.allocate` return
  `Ptr<mut T>`. Heap and Pool allocations are released explicitly with their
  existing `free` operations; Stash allocations are invalidated by `reset` or
  `destroy`. Ptr carries no compiler-enforced cleanup obligation.
- Internal Go identifiers may retain `MutPtr` only when they accurately name
  an internal writability helper and never appear in source, diagnostics,
  generated identifiers, or canonical documentation.

## Slice semantics

```text
Slice<T>       copyable read-only contiguous range
Slice<mut T>   copyable writable contiguous range
```

- Slice never owns, copies, retains, frees, resizes, or otherwise manages its
  elements or backing storage.
- Copying a Slice copies only its pointer and length. All copies address the
  same elements.
- `Slice<T>` permits element reads only.
- `Slice<mut T>` permits element reads and writes.
- `Slice<mut T>` weakens implicitly to `Slice<T>` at the outermost layer only.
  There is no upgrade or nested weakening.
- Multiple Slice aliases, including multiple writable aliases, are permitted.
  Their correct use is the programmer's responsibility.
- Slice has no special storage-position restriction beyond the current View
  position rules. It may be a local, parameter, result, object member, ADT
  payload, union member, Array/Slice/List element, Dict value, Fun
  parameter/result, Task argument/result, or Channel element when T satisfies
  that position's existing rules.
- Existing View's prohibition as a Ptr pointee remains for Slice; changing the
  general pointer-pointee matrix is outside this RFC.
- `Slice<Slice<T>>` and `Slice<Slice<mut T>>` are valid when the inner Slice is
  otherwise storable.
- Slice is not implicitly nullable as a Hexal value. An empty Slice may use a
  null data pointer paired with zero length; no valid operation dereferences it.
- Slice participates in equality, truthiness, printing, iteration, and
  contextual typing exactly where View currently does.

## Representation

```c
/* Slice<T> */
struct {
    const T *data;
    size_t length;
};

/* Slice<mut T> */
struct {
    T *data;
    size_t length;
};
```

- One monomorphized descriptor type is emitted per concrete T and access mode.
- Generated families are `hex_slice_*` and `hex_mut_slice_*`.
- Slice contains no owner, allocator, capacity, generation, reference count,
  provenance, validity, or synchronization field.
- Passing, returning, and storing Slice copies this two-field value.

## Construction

```text
Array<T,N>.slice(start: Integer, end: Integer) -> Slice<T>
Array<T,N>.mut_slice(start: Integer, end: Integer) -> Slice<mut T>
List<T>.slice(start: Integer, end: Integer) -> Slice<T>
List<T>.mut_slice(start: Integer, end: Integer) -> Slice<mut T>
String.bytes() -> Slice<Byte>
String.slice(start: Integer, end: Integer) -> Slice<Byte>
Slice<T>.from_pointer(pointer: Ptr<T> | Ptr<mut T>, length: Size) -> Slice<T>
Slice<mut T>.from_pointer(pointer: Ptr<mut T>, length: Size) -> Slice<mut T>
Slice<T>.empty() -> Slice<T>
Slice<mut T>.empty() -> Slice<mut T>
```

- Integer is the existing signature metavariable for any Hexal integer.
- Bounds normalize to Size, are checked, and use half-open `[start, end)`.
- Array mutable slicing requires a writable Array place.
- A fixed List handle may produce a mutable Slice because existing List
  semantics permit interior element mutation.
- String exposes read-only bytes only.
- Every operand evaluates once, left to right.
- `from_pointer` accepts only the exact non-null pointer types listed and does
  not validate the allocation's length, alignment, initialization, lifetime,
  provenance, or future validity.
- `empty()` and an empty slice of an empty List use null data plus zero length.
- Slicing a temporary Array is rejected because no source place exists.
  Other lifetime validity is the programmer's responsibility.

## Operations

```text
Slice<T>.length() -> Size
Slice<mut T>.length() -> Size
Slice<T>[index: Integer] -> read-only-place<T>
Slice<mut T>[index: Integer] -> writable-place<T>
Slice<T>.slice(start: Integer, end: Integer) -> Slice<T>
Slice<mut T>.slice(start: Integer, end: Integer) -> Slice<mut T>
```

- Indexing traps unless `0 <= index < length` after existing Size
  normalization.
- Re-slicing traps unless both bounds form a valid half-open subrange.
- Re-slicing cannot widen the represented range and preserves access mode.
- Bounds checks validate only the descriptor length. They do not prove that
  data still names live initialized storage.
- Iteration uses the existing one- and two-binder sequence rules.
- Equality compares length then elements; printing and truthiness retain the
  existing View contracts.
- Element reads, writes, equality, and printing use T's existing contracts.

## IO and text APIs

The universal zero-copy byte-range APIs remain, with the renamed type:

```text
String.from_bytes(heap: Heap, bytes: Slice<Byte>) -> String
String.from_runes(heap: Heap, runes: Slice<Rune>) -> String
IO.write(from: Slice<Byte>) -> Size | Error
Ptr<mut Bytes>.write(from: Slice<Byte>) -> Size | Error
```

- String construction synchronously reads the Slice and allocates an
  independent String.
- IO and Bytes retain the existing single-write, cursor, Error, EoS, close,
  self-alias, and scheduler-aware blocking contracts.
- Passing a Slice to a synchronous operation does not transfer or extend its
  lifetime.
- IO lowers directly to the Slice pointer and length without allocation or
  copying.
- Print may continue to use internal pointer-length helpers; no additional
  public write overloads are added.

## API migration

```text
MutPtr<T>                                      -> Ptr<mut T>
View<T>                                        -> Slice<T>
proposed Ref<T>                                -> Ptr<T>
proposed MutRef<T>                             -> Ptr<mut T>
proposed Box<T>                                -> allocator-produced Ptr<mut T>
ref place                                      -> @place
pointer.value (whole-pointee pseudo-member)   -> ^pointer
Array<T,N>.slice(...) -> View<T>               -> Slice<T>
List<T>.slice(...) -> View<T>                  -> Slice<T>
String.bytes() -> View<Byte>                   -> Slice<Byte>
String.slice(...) -> View<Byte>                -> Slice<Byte>
String.from_bytes(heap, View<Byte>)             -> Slice<Byte> parameter
String.from_runes(heap, View<Rune>)             -> Slice<Rune> parameter
IO.write(from: View<Byte>)                      -> Slice<Byte> parameter
MutPtr<Bytes>.write(from: View<Byte>)           -> Ptr<mut Bytes>.write(Slice<Byte>)
```

No ownership wrapper, owning copy operation, source-specific IO overload, or
compatibility alias is introduced.

## Removed lifetime machinery

Delete every compiler path whose sole purpose is deciding whether a View may
outlive or be invalidated by its source:

- local-root and parameter-root provenance;
- nested aggregate return propagation;
- returned-wrapper provenance propagation;
- temporary Array/List root rejection other than the direct requirement that
  Array slicing has an addressable source place;
- source-binding transparency used only for View escape;
- collection/member/pointer stored-View escape detection;
- Task/Channel View-retention rejection based only on source lifetime; and
- resize/reset/free invalidation tracking used only for View.

Bounds checks, element-type checks, writable-source checks, and Bytes
self-alias detection are not lifetime machinery and remain.

## Diagnostics

The earliest proving phase diagnoses:

- invalid Ptr/Slice arity, element/pointee type, or `mut` placement;
- `@` applied to a value that is not an addressable place;
- `^` applied to a non-pointer, a pointer that may be Nil without prior
  narrowing, or `Ptr<Unknown>` before recovery to a concrete pointer type;
- use of removed `MutPtr`, `View`, or `MutView` names, or proposed `Ref`,
  `MutRef`, or `Box` names;
- mutation through `Ptr<T>` or `Slice<T>`;
- read-only-to-writable upgrade or invalid nested weakening;
- invalid construction bounds, pointer mode, or mutable source place;
- out-of-bounds indexing or re-slicing; and
- every existing non-lifetime pointer, element, and API misuse.

No diagnostic rejects a Slice because it may outlive, alias, race with, or be
invalidated by its source. Diagnostics use `@` for address-taking and prefix
`^` for explicit dereference. They never suggest `ref`, pointer `.value`, Ref,
borrow blocks, ownership annotations, or generated C names.

## Required sweep

Inventory and reconcile:

- lexer/parser recognition of prefix `@` and `^`, distinction of prefix `^`
  from binary XOR, retirement of the `ref` keyword and pointer `.value`
  pseudo-member, generic-argument parsing, source formatting, and diagnostic
  spelling;
- Ptr/MutPtr construction, interning, display, canonical keys, positions,
  weakening, unions, narrowing, methods, address-taking, Heap/Stash/Pool APIs,
  volatile operations, pointer returns, and C ABI spelling;
- View/Slice construction, interning, eligibility, positions, nesting,
  operations, contextual typing, equality, truthiness, printing, and iteration;
- Array/List slicing, String APIs, IO, Bytes, print, and scheduler-aware
  blocking paths;
- every View provenance, escape, returned-wrapper, source-root, and
  invalidation path named above;
- generator components, templates, discovery, dependency propagation,
  includes, helper names, and artifact ordering;
- tests, c23validation fixtures, workbench snippets, generated-C assertions,
  and manifest entries containing `MutPtr`, `View`, `MutView`, `hex_view`, Ref,
  Box, borrow, ownership-wrapper, or scoped-lifetime proposals.

## Reference synchronization

After behavior stabilizes and only with explicit user approval, update
`docs/reference.md` once to:

- replace Ptr/MutPtr spelling with Ptr's optional `mut` target;
- replace View with Slice and add Slice's writable mode;
- retain first-class Slice storage, return, nesting, equality, printing,
  iteration, and zero-copy IO contracts;
- replace every affected API and generated-C spelling;
- remove all Slice provenance, escape, and invalidation guarantees;
- state the programmer-owned lifetime rule and its undefined-behavior
  consequence; and
- replace the `ref` keyword with unary `@` as the sole address-taking form;
- replace pointer `.value` with prefix `^` as the sole explicit dereference
  form while retaining one-layer object-pointer member auto-dereference;
- state that Ref, Box, and borrow blocks are not language features; Ptr carries
  neither ownership nor automatic cleanup.

No affine ownership, move, destructor, or automatic-cleanup contract is added.

## Validation

This section is exhaustive.

- `Ptr<T>`, `Ptr<mut T>`, `Slice<T>`, and `Slice<mut T>` parse with exactly one
  complete T. `mut` inside every other generic constructor is rejected.
- `MutPtr`, `View`, `MutView`, `Ref`, `MutRef`, `Box`, and `MutSlice` are
  rejected as unknown type names when no matching user type is declared;
  user-defined types may use the non-reserved names `Ref`, `MutRef`, `Box`, or
  `MutSlice`. `ref` and `borrow` retain ordinary identifier behavior.
- `@fixed_place` produces `Ptr<T>` and `@writable_place` produces `Ptr<mut T>`;
  `@` on a literal, temporary, function declaration, or other non-place is
  rejected by the earliest proving phase.
- Every accepted `ref place` program compiles after mechanical rewriting to
  `@place`, with identical pointer identity, lifetime checks, and generated C.
- `^reader` reads T from `Ptr<T>`; `^writer` reads or writes T through
  `Ptr<mut T>`. Writing through `Ptr<T>` is rejected.
- `^^pointer_to_pointer` dereferences two layers; `@^pointer` addresses the
  resulting place; each layer preserves its independently declared access
  mode.
- Every accepted pointer `.value` program compiles after syntax-aware rewriting
  to prefix `^`, with identical pointer identity, null narrowing, lifetime
  checks, and generated C. Ordinary object members named `value` remain member
  accesses, including through one-layer pointer auto-dereference.
- Prefix `^` on scalar, aggregate, Fun, Slice, or other non-pointer values is
  rejected. Nullable pointers require narrowing and `Ptr<Unknown>` requires
  recovery before dereference.
- Every accepted MutPtr program compiles after mechanical `Ptr<mut T>` rewriting
  with identical checked behavior and C representation.
- Pointer weakening remains outermost-only; upgrades and nested weakening fail.
- Address-taking, Heap/Stash/Pool APIs, volatile access, functions, methods, unions,
  diagnostics, and generated source use the new Ptr spelling.
- Stack addresses, allocator-produced addresses, and foreign addresses use the
  same Ptr family. No generated representation, checker fact, or cleanup path
  distinguishes a proposed Ref or Box.
- Every valid View program compiles after mechanical `View<T>` to `Slice<T>`
  rewriting unless it deliberately requires writable Slice access.
- Slice works in every former View position, including bindings, results,
  aggregates, collections, function values, Tasks, and Channels.
- Nested Slice descriptors are accepted; Ptr-to-Slice retains the existing
  pointer-pointee rejection.
- Slice copies alias the same elements and keep independent descriptor values.
- Slice<mut T> writes update the shared source and aliases; Slice<T> rejects
  writes.
- Multiple aliases, including writable aliases, compile without overlap
  diagnostics.
- Array/List read-only and writable slicing preserve exact ranges; invalid
  bounds trap.
- String bytes/slices preserve exact UTF-8 byte ranges and remain read-only.
- Slice indexing, re-slicing, equality, printing, truthiness, and iteration
  retain existing View behavior for read-only Slice and apply consistently to
  writable Slice.
- `from_pointer` accepts exact pointer modes without provenance or lifetime
  analysis; bounds checks still apply to later Slice access.
- Empty Slice uses null data and zero length; non-empty constructors use
  non-null data.
- Direct local-rooted, nested-aggregate, collection-stored, parameter-rooted,
  and Task/Channel-returned Slices receive no lifetime diagnostic.
- List growth/free, String free, allocator reset/destroy, and raw allocation
  release receive no Slice-invalidation diagnostic.
- IO and Bytes consume Slice<Byte> without an intermediate allocation or copy
  and retain all non-lifetime behavior.
- Generated C contains exactly the two pointer-plus-length layouts and no
  Slice lifetime, provenance, ownership, or synchronization metadata.
- Removed lifetime-analysis paths have no remaining production callers.
- Active source, diagnostics, tests, snippets, generated artifacts, and the
  approved canonical reference retain no old public spelling.
- Existing manifest hashes move only for deliberate Ptr/Slice spelling,
  writable-Slice support, removed lifetime diagnostics, and renamed generated
  components.

## Detailed implementation plan

### Phase 1: inventory and baseline

1. Record every production/test/snippet/reference occurrence of MutPtr, View,
   `hex_view`, the `ref` keyword, pointer `.value`, Ref, Box, ownership
   wrappers, and View lifetime/provenance handling.
2. Capture the snippet manifest and qualified generated-C baseline.
3. Classify each current View check as a retained type/bounds rule or deleted
   lifetime/provenance rule before editing.

### Phase 2: pointer spelling

1. Add the `@` token and prefix address expression; retire `ref` as a keyword.
2. Parse prefix `^` as dereference in unary position while retaining binary
   `^` as XOR; retire pointer `.value` in parsing and checking.
3. Parse optional `mut` only in Ptr and Slice type arguments.
4. Preserve internal pointer identity and writability while replacing every
   user-visible MutPtr, `ref`, and pointer `.value` spelling.
5. Keep one-layer object-pointer member auto-dereference; make a member named
   `value` ordinary and unambiguous.
6. Mechanically migrate source programs, tests, fixtures, and snippets using
   parsed pointer meaning rather than a blind `.value` text replacement.
7. Verify identical generated C for pointer-only programs apart from Hexal
   source mappings and diagnostics that spell the new syntax.

### Phase 3: Slice identity and writable mode

1. Rename View identity, display, canonical keys, component families, APIs,
   diagnostics, tests, and snippets to Slice.
2. Add Slice<mut T>, mutable Array/List slicing, writable indexing, re-slicing,
   and outermost weakening.
3. Preserve first-class positions, copying, nesting, equality, truthiness,
   printing, iteration, and contextual typing.
4. Migrate IO, Bytes, String, and every component dependency.

### Phase 4: remove lifetime analysis

1. Remove local-root return checks and nested/wrapper return propagation.
2. Remove stored-View escape and source-root invalidation checks.
3. Remove provenance fields and transformations used only by those checks.
4. Retain bounds, element, mutability, pointer-mode, and Bytes self-alias checks.
5. Verify every removed helper has no production caller.

### Phase 5: lowering and conformance

1. Emit read-only and writable Slice pointer-plus-length families once each per
   concrete T and update component/include selection.
2. Implement every Validation item with focused parser/checker tests,
   integration tests, generated-C assertions, and qualified C23 cases.
3. Run ordinary and tagged suites and review every manifest movement against
   the allowed blast radius.
4. With explicit user approval, synchronize `docs/reference.md` once.
5. Update status and superseded-spec dispositions only after implementation and
   reference agree.
6. Rebuild and restart the workbench before handoff.

## Open questions

None.
