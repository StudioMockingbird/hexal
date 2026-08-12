# RFC 0047: Stable References and C Pointers

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; pending approval
- Features: `Ref<T>`, `MutRef<T>`, `CPtr<T>`, `MutCPtr<T>`, heap-only native
  references, explicit C trust boundaries, View provenance, reference receiver
  dispatch, Atomic placement, and concurrency sharing
- Created: 2026-08-12
- Revised: 2026-08-12
- Depends on: RFC 0007 (pointer mutability), RFC 0010 (non-null pointers), RFC
  0020 (Array and View), RFC 0026 (Heap allocation), RFC 0033 (restricted
  pointers), RFC 0035 (C-style copying and manual lifetimes), RFC 0037
  (`Atomic<T>`), RFC 0043 (pointer-plus-length Views), and RFC 0046 (reference
  reconciliation)
- Coordinates with: RFC 0027 (Arena and Pool allocators), RFC 0039 (C
  interoperability), `docs/reference.md`, and `docs/grammar.ebnf`

## 1. Summary

Hexal separates native references from foreign C pointers.

| Type | Meaning |
| --- | --- |
| `Ref<T>` | non-null, read-only reference admitted to native Hexal APIs |
| `MutRef<T>` | non-null, writable reference admitted to native Hexal APIs |
| `CPtr<T>` | non-null, read-only raw C pointer |
| `MutCPtr<T>` | non-null, writable raw C pointer |

Native code uses `Ref` and `MutRef`. C imports, exports, callbacks, and other
explicit ABI boundaries use `CPtr` and `MutCPtr`.

Native-created `Ref` and `MutRef` values may never designate automatic storage.
Local values, parameters, value receivers, binders, and temporaries cross
boundaries only by value. Sharing by reference requires explicit
allocator-managed storage. An explicitly asserted conversion may admit foreign
storage to a native API, without changing its foreign ownership.

```hexal
value: Int32 = 10
ref value
-- Error: automatic storage cannot be referenced

h: Heap = Heap.new()
shared: MutRef<Int32> = h.allocate<Int32>(value)
reader: Ref<Int32> = shared
```

`CPtr` and `MutCPtr` mark the C trust boundary. Their provenance, lifetime,
retention, and deallocation rules come from the foreign contract. They do not
weaken the native `Ref` invariant.

This RFC replaces the source type names `Ptr<T>` and `MutPtr<T>`. Neither old
name remains as an alias.

## 2. Goals

- Make the idiomatic Hexal reference type unmistakable.
- Prevent native references to stack storage without escape analysis.
- Keep allocation and sharing explicit.
- Make C pointer use visible in types and signatures.
- Preserve read-only versus writable pointee capability for both families.
- Preserve direct C ABI lowering without implicit allocation or marshalling.
- Keep one obvious conversion at each native/foreign boundary.

## 3. Non-goals

- Ownership, borrow checking, regions, or general lifetime inference.
- Exclusive mutable references. `MutRef` permits aliases.
- Implicit heap promotion or compiler-selected allocation.
- Garbage collection or reference counting.
- A call-scoped stack-reference exception for native or C calls.
- Preventing use-after-free, collection invalidation, foreign lifetime bugs,
  or data races after a valid reference has been created.
- Full C pointer arithmetic, integer-address conversion, or an `unsafe` block.
- Implementing C interop, Arena, or Pool in this RFC.

## 4. Terminology and storage

### 4.1 Automatic storage

Automatic storage is inline storage tied to a generated C block or call frame:

- root executable bindings, which lower to locals in generated `main`;
- function and method locals;
- parameters passed by value;
- value-receiver `self`;
- `for` binders;
- compiler-materialized temporaries; and
- members or elements transitively rooted in any item above.

`mut` changes whether a binding or member can be replaced. It does not change
storage duration.

### 4.2 Allocator-managed native storage

Allocator-managed native storage has an address that remains valid until an
explicit invalidating operation:

- a live `Heap.allocate<T>` allocation;
- a live future `Arena.allocate<T>` allocation;
- a live future `Pool<T>.allocate` slot;
- a member or Array element rooted in one of those allocations; and
- eligible collection backing storage until an operation relocates or frees it.

RFC 0027's deferred stack-backed Arena idea is incompatible with this RFC. A
stack-backed region could not produce a `Ref`, `MutRef`, or View.

### 4.3 Foreign storage

Foreign storage enters through explicit C interop:

- an imported C pointer result or parameter;
- an imported external object;
- a C callback argument; or
- storage reached through one of those values.

Hexal cannot prove that foreign storage is heap allocated, aligned, live, or
retained correctly. `CPtr` and `MutCPtr` expose that trust boundary rather than
pretending the native reference invariant applies.

### 4.4 Value location versus target location

All four reference/pointer values are ordinary shallow-copyable scalar values.
The value itself may live in an automatic local, object, collection, or
allocator allocation. The restriction applies to what it designates.

A `Ref` normally designates allocator-managed native storage. Section 11.2
also permits an explicit trust assertion that treats foreign storage as stable
enough for a native API. That assertion changes neither provenance nor
ownership and cannot make native deallocation valid.

```hexal
shared: MutRef<Int32> = h.allocate<Int32>(10)
copy: MutRef<Int32> = shared
-- Both reference values are local. Their common target is allocated.
```

## 5. Native references

### 5.1 `Ref<T>`

- Non-null and non-owning.
- Reads a `T` in storage admitted to native Hexal APIs.
- Does not permit mutation through that reference.
- Copies by copying the address.
- Becomes dangling when the designated storage is freed or invalidated.

### 5.2 `MutRef<T>`

- Non-null and non-owning.
- Reads and writes a `T` in storage admitted to native Hexal APIs.
- Copies by copying the address.
- May have aliases. “Mut” means write-capable, not unique or exclusive.
- Weakens implicitly to `Ref<T>` at the outermost layer only.

```hexal
writer: MutRef<Int32> = h.allocate<Int32>(0)
reader: Ref<Int32> = writer
```

Upgrading `Ref<T>` to `MutRef<T>` and weakening nested layers remain invalid.

### 5.3 Eligible pointees

The current reference's pointee eligibility rules are renamed, not broadened:

- complete scalars, objects, ADTs, Arrays, pointer/reference cells, and other
  eligible complete types may be referenced;
- `Unknown` remains an incomplete type permitted only behind a reference or C
  pointer;
- `String`, `List`, `Dict`, `Stream`, and `View` remain ineligible native
  reference targets because they are already handles/descriptors;
- `Fun<...>` remains ineligible; and
- `Atomic<T>` remains non-addressable with `ref`.

Nested references are permitted when every layer obeys its own rules:

```hexal
value: MutRef<Int32> = h.allocate<Int32>(1)
cell: MutRef<MutRef<Int32>> = h.allocate<MutRef<Int32>>(value)
```

### 5.4 Dereference and nullability

- `.value` dereferences `Ref<T>` and `MutRef<T>`.
- A `MutRef` pointee is writable; a `Ref` pointee is not.
- Nullability remains explicit: `Ref<T> | Nil` or `MutRef<T> | Nil`.
- A nullable reference must be narrowed before dereference.
- Nullable native references retain the null niche and require no allocation
  or union tag.
- Reference arithmetic, indexing, ordering, subtraction, integer conversion,
  bit casting, and one-past values remain unavailable.

## 6. `ref`

`ref place` creates only a native reference. It never creates a C pointer.

### 6.1 Accepted roots

The checker accepts `ref place` only when:

- the final type is eligible for `Ref`/`MutRef`; and
- the place is rooted in stable native storage.

Accepted roots are:

- a live allocator allocation reached through `.value`;
- a member or Array element transitively rooted in such an allocation;
- an eligible List or View element under section 8; and
- stable native storage reached through an existing `Ref` or `MutRef`.

The result follows place capability: a writable place yields `MutRef<T>` and a
fixed place yields `Ref<T>`.

```hexal
type Point = { mut x: Int32, y: Int32, }

point: MutRef<Point> = h.allocate<Point>(Point { x = 1, y = 2 })
x: MutRef<Int32> = ref point.x
y: Ref<Int32> = ref point.y
```

Pointer-style member access continues to auto-dereference, so `ref point.x` is
rooted in the allocation designated by `point`.

### 6.2 Rejected roots

`ref` rejects:

- root executable and local bindings;
- by-value parameters;
- value-receiver `self`;
- `for` binders;
- temporaries;
- members or Array elements rooted in any item above;
- a local reference or C-pointer cell itself; and
- foreign storage, which remains represented by `CPtr`/`MutCPtr`.

```hexal
mut local: Point = Point { x = 1, y = 2 }
ref local                  -- Error
ref local.x                -- Error

shared: MutRef<Point> = h.allocate<Point>(local)
ref shared.value           -- valid
ref shared.x               -- valid
ref shared                 -- Error: addresses the local reference cell
```

The checker classifies the root storage, not merely the final member or index.
There is no exception for a reference that appears not to escape.

### 6.3 No implicit promotion

`ref local` never allocates. Calls, method dispatch, Views, and C interop never
silently box a value.

```hexal
local: Point = Point { x = 1, y = 2 }
shared: MutRef<Point> = h.allocate<Point>(local)
defer h.free(shared)
consume(shared)
```

Mutating `shared` mutates the allocated copy, not `local`.

## 7. C pointers

### 7.1 `CPtr<T>` and `MutCPtr<T>`

- `CPtr<T>` is a non-null raw C pointer that does not permit mutation through
  that pointer.
- `MutCPtr<T>` is a non-null raw C pointer that permits mutation.
- `MutCPtr<T>` weakens implicitly to `CPtr<T>` at the outermost layer only.
- Both copy the address and carry no ownership.
- Their provenance, extent, alignment, lifetime, and retention contract are
  not verified by Hexal.
- “Raw” means foreign and unchecked, not that every C operation is available.

Nullability remains explicit:

```hexal
raw: MutCPtr<c.Widget> | Nil = widget.open()
```

### 7.2 Creation

Ordinary Hexal code obtains a C pointer only from:

- an imported C declaration;
- a C callback or exported-function parameter;
- an imported external object;
- another C pointer operation explicitly specified by the C interop RFC; or
- explicit conversion from a native reference.

There is no `c_ref local`, address-of-local conversion, integer-to-pointer
conversion, or implicit native-to-C conversion.

### 7.3 Operations

V1 C pointers support only what interop requires:

- null checks and narrowing;
- outermost writable-to-read-only weakening;
- `.value` and imported-field access when the pointee is complete;
- pointer identity equality; and
- explicit boundary conversion described below.

Source pointer arithmetic, indexing, ordering, subtraction, integer conversion,
bit casting, arbitrary casts, and one-past values remain unavailable. A later
C-interop RFC may add narrowly justified operations without changing native
references.

The existing one-layer `Unknown` erasure/recovery rule applies within each
family. It never converts between native references and C pointers. Recovery
is explicit and asserts the concrete pointee type expected by the foreign
contract.

### 7.4 Pointee types

C pointers may name:

- Hexal ABI-compatible scalar and complete foreign record types;
- incomplete or opaque foreign types;
- `Unknown` for C `void`; and
- recursively mapped C pointer layers.

They do not make Hexal runtime handles, ADTs, structural unions, or other
unsettled layouts valid C ABI values.

## 8. Views and implicit borrows

### 8.1 Storage rule

`View<T>` contains an address and length even though it is not a reference type.
Allowing a View into automatic storage would recreate a stack reference under
another spelling. A View may designate only:

- stable native storage;
- static immutable storage exposed by a built-in, such as String literal
  bytes; or
- explicitly trusted foreign storage.

The View descriptor remains an inline, shallow-copyable value and retains the
reference's general storability rule.

### 8.2 Array slices

Slicing an automatic, parameter, or temporary Array is invalid, even for a
local-only View. Arrays cross boundaries by copying.

```hexal
values: Array<Int32, 4> = [1, 2, 3, 4]
head: View<Int32> = values.slice(0, 2)
-- Error: View cannot designate automatic Array storage

stored: MutRef<Array<Int32, 4>> =
    h.allocate<Array<Int32, 4>>([1, 2, 3, 4])
head: View<Int32> = stored.value.slice(0, 2) -- valid
```

A function that needs a borrowed sequence receives an existing View or a
reference to stable storage, not an Array by value followed by slicing.

### 8.3 Constructors

- `View<T>.from_ref(reference, length)` replaces
  `View<T>.from_pointer(pointer, length)` for native storage.
- It accepts `Ref<T>` or `MutRef<T>` and weakens mutation capability in the
  resulting read-only descriptor.
- `View<T>.from_c_pointer(pointer, length)` accepts `CPtr<T>` or `MutCPtr<T>`
  and is an explicit trust assertion.
- `from_c_pointer` does not validate provenance, bounds, alignment, lifetime,
  or initialized extent.
- `View<T>.empty()` remains valid.
- No constructor allocates or copies elements.

The old checker rule tracing `ref`-derived pointers inside
`View.from_pointer` is removed. Invalid stack references cannot be formed, and
foreign conversion is visibly trusted.

### 8.4 Return and storage

- A valid View may be copied, stored, passed, or returned.
- The special rejection of a returned View rooted in a local becomes
  unnecessary because a valid View cannot designate automatic storage.
- Retaining a View past Heap free, Arena reset/destroy, Pool free, List
  relocation, String free, or foreign invalidation remains programmer error.

### 8.5 Collections

- `List<T>.slice` remains valid because the backing region is allocated.
- `ref list[index]` is valid when `T` is native-reference eligible.
- Either result is invalidated by List resize, structural mutation, or free.
- Element replacement preserves the slot address but changes its value.
- `ref view[index]` may produce `Ref<T>` when `T` is eligible. Its lifetime is
  bounded by the View's source region.
- Dict exposes no element place and therefore gains no `ref` form.

These invalidations remain manual lifetime rules. The compiler need not track
aliases or reject a later invalidating operation.

### 8.6 Other descriptors

- `RuneCursor` remains valid because it borrows static or allocated String
  payload, not inline String-handle storage.
- List-backed Stream lifetime rules remain unchanged.
- Internal runtime addresses that are not observable as source `Ref`, `CPtr`,
  or View values are unaffected.

## 9. Functions and methods

### 9.1 Signatures

- Native APIs use `Ref<T>` and `MutRef<T>`.
- Foreign-facing APIs use `CPtr<T>` and `MutCPtr<T>`.
- Non-reference parameters and results retain value-copy semantics.
- A callee cannot take `ref` of a by-value parameter.
- Passing or returning any reference/pointer copies its address.

```hexal
fun inspect(value: Point)
    ref value -- Error: parameter is automatic storage
end

fun inspect_shared(value: Ref<Point>)
    print(value.x)
end
```

### 9.2 Receiver declarations

User method targets become nominal `T`, `Ref<T>`, or `MutRef<T>`. `CPtr` and
`MutCPtr` are not user `impl` targets; foreign functions remain qualified C
operations rather than Hexal methods.

```hexal
impl MutRef<Point>.move(dx: Int32)
    self.x = self.x + dx
end
```

### 9.3 Receiver adaptation

Adaptation order becomes:

1. exact target;
2. outermost `MutRef<T>` weakening to `Ref<T>`;
3. reference dereference to a copied value receiver; or
4. implicit `ref` only from a place that explicit `ref` may address.

A local object cannot call a reference-receiver method through implicit `ref`.

```hexal
local: Point = Point { x = 1, y = 2 }
local.move(1)       -- Error

shared: MutRef<Point> = h.allocate<Point>(local)
shared.move(1)      -- valid
```

A value-receiver method remains the way to operate on an inline value without
allocation. It receives a copy and cannot mutate the caller.

## 10. Allocation and cleanup

### 10.1 Allocator APIs

- `Heap.allocate<T>(initial) -> MutRef<T>`.
- `Heap.free` accepts `Ref<T>` or `MutRef<T>` from that Heap.
- `Heap.free` rejects `CPtr<T>` and `MutCPtr<T>`.
- Future `Arena.allocate<T>` and `Pool<T>.allocate` return `MutRef<T>`.
- Arena/Pool cleanup accepts only references with matching native provenance.
- A C deallocator accepts C pointers, not native references, unless a future
  explicit allocator-compatibility contract says otherwise.

Allocator identity, alias invalidation, manual cleanup, and shallow copying are
otherwise unchanged.

### 10.2 Atomic placement

The current reference shares an automatic Atomic-containing object with
`ref shared`. That becomes invalid. Allocator initialization therefore gains
one narrow direct-placement rule:

- `Atomic<T>.new(...)` may directly initialize its final allocator-managed
  destination;
- an object containing Atomic members may be constructed directly there;
- no Atomic is copied or moved through an ordinary argument value; and
- passing an existing Atomic-containing binding to `allocate` remains invalid.

```hexal
type Shared = { count: Atomic<Int32>, }

shared: MutRef<Shared> = h.allocate<Shared>(
    Shared { count = Atomic<Int32>.new(0) }
)
defer h.free(shared)
```

`Heap.allocate` is a compiler-known placement operation for this case, not an
ordinary by-value call parameter. Future Arena and Pool allocation follow the
same rule.

## 11. Native/C conversions

Conversions are explicit in both directions. Assignment, arguments, returns,
and union injection never convert between the families implicitly.

### 11.1 Native reference to C pointer

```hexal
native: MutRef<Int32> = h.allocate<Int32>(10)
raw: MutCPtr<Int32> = native.to_c_pointer()
readonly: CPtr<Int32> = raw
```

- `Ref<T>.to_c_pointer() -> CPtr<T>`.
- `MutRef<T>.to_c_pointer() -> MutCPtr<T>`.
- Conversion preserves the address and performs no allocation or copy.
- The native allocation remains owned and freed by its original allocator.
- The caller must keep it live while C may use or retain the pointer.
- The C callee must not free it without an explicit allocator contract.

### 11.2 C pointer to native reference

```hexal
raw: MutCPtr<c.Node> | Nil = c_api.node()

if raw != nil
    node: MutRef<c.Node> = MutRef.from_c_pointer(raw)
end
```

- `Ref<T>.from_c_pointer(CPtr<T>) -> Ref<T>`.
- `MutRef<T>.from_c_pointer(MutCPtr<T>) -> MutRef<T>`.
- The input must first be statically non-null.
- Conversion is a programmer assertion that the storage has stable lifetime,
  correct alignment, initialized extent, compatible representation, and the
  claimed mutation capability.
- It does not allocate, copy, validate, or transfer ownership.
- A resulting native reference must not be passed to `Heap.free` unless its
  target really came from that Heap. Conversion does not manufacture allocator
  ownership; passing a foreign-backed reference to native cleanup is an
  allocator-provenance error under the existing manual-lifetime rules.

The last rule means a foreign pointer converted for native APIs normally keeps
foreign cleanup. `Ref` describes access and stability, not ownership.

### 11.3 Why conversions are not implicit

- Implicit native-to-C conversion would hide pointer retention and foreign
  mutation at ordinary calls.
- Implicit C-to-native conversion would falsely imply proven lifetime and
  provenance.
- Explicit conversion makes code review locate every trust-boundary crossing.

## 12. Concurrency

- `spawn` continues to shallow-copy every argument.
- Inline locals, Arrays, objects, and ADTs cross Task boundaries only by value.
- Shared native state must be allocator-managed and passed as `Ref` or
  `MutRef`.
- Atomic-containing shared objects use direct allocator placement.
- A C pointer may cross a Task boundary only when its foreign contract permits
  retention and concurrent access.
- Reference stability does not make unsynchronized mutation safe.
- Existing Mutex, Atomic, Channel, scheduler, and data-race rules remain.
- A Task must not outlive native or foreign storage it accesses.

This rejects `spawn worker(ref local)` while preserving by-value Task arguments.

## 13. C interoperability

RFC 0039 remains a draft. Its eventual design must follow this section.

### 13.1 Imported pointer mapping

| C type | Hexal type |
| --- | --- |
| `const T *` | `CPtr<T> | Nil` |
| `T *` | `MutCPtr<T> | Nil` |
| `const void *` | `CPtr<Unknown> | Nil` |
| `void *` | `MutCPtr<Unknown> | Nil` |
| `T **` | recursively mapped C-pointer layers |

A trusted header contract may strengthen nullability. C `const` controls
read-only versus writable pointer capability; it does not map to `Ref`.

### 13.2 Imported and exported declarations

- Imported C functions use C-pointer types in their checked signatures.
- Imported complete records may be accessed through C pointers when their ABI
  layout is verified.
- Imported opaque records exist only behind C pointers.
- C callback parameters use C-pointer types.
- Exported Hexal functions expose C-pointer types to the C ABI.
- Native-only functions should use `Ref`/`MutRef`, including wrappers around C.

```hexal
fun use_widget(widget: Ref<c.Widget>): Int32
    raw: CPtr<c.Widget> = widget.to_c_pointer()
    return c_api.use_widget(raw)
end
```

### 13.3 Passing native storage to C

A C call cannot borrow an automatic Hexal value, even synchronously.

```hexal
mut result: Int32 = 0
c_api.read_value(ref result) -- Error before type conversion

result: MutRef<Int32> = h.allocate<Int32>(0)
defer h.free(result)
raw: MutCPtr<Int32> = result.to_c_pointer()
status: Int32 = c_api.read_value(raw)
value: Int32 = result.value
```

The foreign contract must guarantee that C:

- accepts the supplied representation and alignment;
- does not read or write outside the allocated object/buffer;
- does not mutate through `CPtr`;
- does not free Hexal-managed storage; and
- does not retain the pointer longer than the native allocation remains live.

### 13.4 C results and ownership

- C pointer results remain C pointers until explicitly converted.
- Null must be narrowed before dereference or conversion.
- A foreign allocation must be paired with its documented C deallocator.
- Converting it to `Ref` does not make `Heap.free` valid.
- `Heap.free`, Arena cleanup, Pool cleanup, and collection cleanup never release
  foreign allocations.

### 13.5 Buffers and Views

- A local Array cannot be exposed as a C buffer.
- Use an allocator-managed Array, eligible List backing region, or C-owned
  allocation.
- Convert a native reference to a C pointer explicitly before a C call.
- No View implicitly converts to a C pointer or C ABI slice.
- A C pointer-plus-length result becomes a View only through
  `View.from_c_pointer(pointer, length)`.
- C output parameters and buffers require explicit stable allocation, followed
  by value copying when an inline Hexal result is desired.

A C pointer-to-pointer output parameter requires an allocated pointer cell.
The exact recursive nullable type comes from the imported declaration; `ref`
of a local C-pointer binding is still invalid.

```hexal
slot: MutRef<MutCPtr<c.Widget> | Nil> =
    h.allocate<MutCPtr<c.Widget> | Nil>(nil)
raw_slot: MutCPtr<MutCPtr<c.Widget> | Nil> = slot.to_c_pointer()
status: Int32 = c_api.create_widget(raw_slot)
result: MutCPtr<c.Widget> | Nil = slot.value
```

C pointer-style APIs may therefore require an allocation where C would take the
address of a local. This cost and ceremony are intentional.

### 13.6 Text

- Runtime String payload is allocator-managed; String literals use static
  storage.
- A future explicit read-only C-string operation may produce `CPtr<Byte>` while
  enforcing String's embedded-NUL and retention rules.
- Strand is inline and cannot expose its address. Passing it to C requires an
  explicit copy to stable storage, such as conversion to runtime String or a
  future buffer operation.
- The compiler never allocates that copy silently.
- Mutable C text never receives String or Strand storage.

### 13.7 Callbacks and contexts

- A callback context cannot point to a Hexal local.
- Native callback state is allocated, converted to `MutCPtr<Unknown>`, and kept
  live for every callback.
- C-owned callback state remains a C pointer.
- A callback may explicitly convert a C pointer to `Ref` only when the library
  contract guarantees stable access for the required duration.

### 13.8 No call-scoped exception

This RFC does not add a special stack pointer valid only during one C call.
Such a feature would introduce another lifetime class and require escape rules
for arguments, callbacks, retention annotations, and indirect calls.

If allocation cost later proves unacceptable, a distinct non-storable,
call-scoped FFI borrow requires its own RFC. It must not weaken `Ref` or make
ordinary `CPtr` creation from locals possible.

## 14. Volatile and low-level access

The current reference places volatile methods on `Ptr`/`MutPtr`. They move to
the C-pointer family:

- `CPtr<T>.read_volatile() -> T`;
- `MutCPtr<T>.read_volatile() -> T`; and
- `MutCPtr<T>.write_volatile(value)`.

`T` retains the existing fixed-width integer, Byte, or Size restriction.
Volatile access remains non-atomic and supplies no synchronization, fence,
device-order, lifetime, or address-validity guarantee.

Native references do not expose volatile operations. MMIO and externally
observable addresses belong to the explicit low-level C-pointer boundary.

## 15. Impact on existing language features

### 15.1 Source-breaking changes

- `Ptr<T>` is renamed to `Ref<T>` for native APIs and storage.
- `MutPtr<T>` is renamed to `MutRef<T>`.
- Old names do not remain aliases.
- Every `ref` of automatic storage becomes an error.
- Pointer-receiver methods become reference-receiver methods and cannot
  auto-borrow locals.
- A View cannot borrow a local or parameter Array.
- `View.from_pointer` splits into `from_ref` and `from_c_pointer`.
- Heap, future Arena, and future Pool allocation return `MutRef`.
- Atomic sharing through `ref` of a local object becomes direct heap placement.
- Task sharing of locals by pointer becomes allocation or value copying.
- C signatures use `CPtr`/`MutCPtr`, with explicit conversion at wrappers.
- Volatile access moves to the C-pointer family.
- C out-parameter and stack-buffer patterns require allocated storage.

### 15.2 Protected names and grammar

- `Ref`, `MutRef`, `CPtr`, and `MutCPtr` become protected type constructors.
- `Ptr` and `MutPtr` cease to be built-in or protected names.
- The grammar's pointer/reference type productions must use all four new names.
- `ref` remains the sole native reference-formation keyword.
- No C-pointer address-taking syntax is added.

### 15.3 Unchanged behavior

- Explicit nullability through `| Nil`.
- Outermost writable-to-read-only weakening within each family.
- `.value` dereference and pointer-style foreign record/member access.
- Value copying for scalars and inline aggregates.
- Shallow handle/reference/pointer copying.
- Explicit allocation and manual lifetime management.
- Object member mutability and reference-rooted writes.
- Collection shallow storage and cleanup rules.
- Function values, generics, unions, ADTs, match, operators, errors, files,
  control flow, and C23 layouts.

### 15.4 Simplifications

- Type names reveal whether code is native or crossing a C trust boundary.
- Stack-reference escape analysis disappears because formation is rejected.
- View checking becomes construction-time provenance checking.
- `View.from_pointer` no longer needs partial tracing through local bindings.
- Task sharing has one native rule: values copy; allocated state may be
  referenced.
- Native and foreign deallocators accept distinct pointer families.

### 15.5 Remaining risks

This RFC does not prevent:

- use after native free;
- stale Arena/Pool references;
- stale references or Views after List relocation;
- double-free, allocator mismatch, or leaks;
- invalid, dangling, misaligned, or undersized C pointers;
- C retaining a converted native pointer too long;
- data races through `MutRef` or `MutCPtr`; or
- misuse after an explicit C-pointer-to-reference assertion.

The reference's C-style manual-lifetime model remains authoritative.

## 16. Diagnostics

The checker reports errors at formation, conversion, receiver adaptation, or
call checking before generation.

```text
[Type Error] ref cannot reference automatic storage; allocate the value first
[Type Error] ref cannot reference a parameter; parameters are passed by copy
[Type Error] MutRef receiver requires stable native storage
[Type Error] View cannot borrow an automatic Array; allocate or copy it
[Type Error] CPtr does not implicitly convert to Ref; use Ref.from_c_pointer
[Type Error] Ref does not implicitly convert to CPtr; use to_c_pointer
[Type Error] Heap.free requires a native Ref, not a C pointer
```

Nested `ref` diagnostics should name the root that determines storage duration.

## 17. C23 lowering

The four source types share ordinary C pointer representation but retain
distinct checked identities:

| Hexal | C23 |
| --- | --- |
| `Ref<Int32>` | `const int32_t *` |
| `MutRef<Int32>` | `int32_t *` |
| `CPtr<Int32>` | `const int32_t *` |
| `MutCPtr<Int32>` | `int32_t *` |

- Conversion between families emits no bits and no cast when C types already
  agree, but remains an explicit checked source operation.
- Source `ref` never lowers to `&` of an automatic local.
- `ref` of stable storage lowers to its native address.
- Hidden C ABI addresses used to pass or return values are not observable as
  Hexal references/pointers and do not violate this RFC.
- Runtime helpers may use internal addresses but must not expose automatic
  storage as `Ref`, `CPtr`, or View data.
- Atomic-containing allocation constructs directly in the final destination.
- No implicit allocation is introduced by reference formation, conversion,
  calls, methods, Views, or C interop.

Nested C const qualification continues to derive from each type layer. The
generator must never discard pointee qualification implicitly.

## 18. Conformance requirements

An implementation conforms when:

1. `Ptr` and `MutPtr` are no longer language types;
2. native APIs use `Ref`/`MutRef` and C ABI APIs use `CPtr`/`MutCPtr`;
3. no ordinary native formation creates a reference or C pointer to automatic
   storage; explicitly trusted foreign conversion remains outside that proof;
4. nested `ref` checks root storage;
5. method adaptation obeys the same rule as explicit `ref`;
6. Views cannot designate automatic Array or Strand storage;
7. native/C conversions are explicit, allocation-free, and capability-safe;
8. native and foreign deallocation domains remain distinct;
9. Atomic-containing shared objects construct directly in allocated storage;
10. C calls cannot receive a pointer formed from a local or temporary;
11. volatile access belongs only to C pointers;
12. invalid cases fail with structured diagnostics before generation; and
13. generated C exposes no automatic address through a source reference,
    C pointer, or View.

## 19. Reference changes if accepted

Acceptance requires coordinated updates to `docs/reference.md`:

- **Status and resolution chain:** record RFC 0047's supersession.
- **Protected names:** replace `Ptr`/`MutPtr` with all four constructors.
- **Bindings, places, and copying:** state that automatic values cross
  boundaries only by copy.
- **Value classification:** rename pointer handle references and distinguish C
  pointers as foreign external state.
- **Aliases and objects:** rename pointer-indirect recursion and member access.
- **Pointers and nullability:** replace the section with native references,
  C pointers, provenance, conversions, and nullability.
- **Functions and methods:** use reference receiver targets and restricted
  implicit `ref`.
- **Generics, unions, equality, and printing:** apply the four distinct
  canonical types and their eligibility rules.
- **View:** split `from_pointer`, prohibit automatic roots, and remove partial
  escape tracing.
- **Allocation:** return/free native references and define Atomic placement.
- **Streams:** change producer callback state to `MutRef<State>`.
- **Tasks and Atomic:** use allocated shared state.
- **Layout and volatile:** move volatile operations to C pointers.
- **C23 contract:** define all four lowering identities and forbid exposed
  automatic addresses.
- **Pending C interop:** record RFC 0039's required C-pointer mapping.

`docs/grammar.ebnf` must replace `Ptr`/`MutPtr` type constructors with `Ref`,
`MutRef`, `CPtr`, and `MutCPtr` when this RFC is accepted. The `ref` expression
grammar remains unchanged.

## 20. Supersession

If accepted, this RFC supersedes these current reference decisions:

- the source names and protected constructors `Ptr` and `MutPtr`;
- `ref place` over any addressable place;
- implicit pointer-receiver adaptation from any addressable value;
- Views rooted in local or parameter Arrays;
- `View.from_pointer` and its local-origin tracing;
- the special local-root View return check;
- volatile operations on the native pointer family; and
- Atomic sharing through a pointer to an automatic object.

It also replaces RFC 0039's proposed C mapping from `Ptr`/`MutPtr` to
`CPtr`/`MutCPtr`.

All unrelated rules in the current normative reference remain in force.

## 21. Decision

This draft adopts the following coupled design:

1. Idiomatic Hexal uses `Ref<T>` and `MutRef<T>`.
2. Native references never designate automatic storage.
3. C boundaries use `CPtr<T>` and `MutCPtr<T>`.
4. Read-only versus writable capability exists in both families.
5. Conversion between families is explicit in both directions.
6. Views obey the stable-storage rule.
7. No synchronous C-call stack-borrow exception exists.
8. Raw C pointers remain deliberately limited to specified interop operations;
   they are not a general escape hatch from Hexal's semantics.
