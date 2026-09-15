# RFC 0191: Advanced C Interoperability

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Scope inventory only;
  each capability requires a focused child RFC before implementation
- Created: 2026-09-15
- Scope: retain the advanced C-interoperability capabilities deliberately
  excluded from RFC 0039's first compiler-core boundary
- Depends on: implemented RFC 0039 and the concrete library that demonstrates
  demand for each capability
- Coordinates with: RFC 0155 (`unsafe do ... end`), RFC 0156 (raw pointer
  operations), RFC 0168 (libuv capability ownership), RFC 0186 (standard-library
  boundary), and the filesystem/build driver
- Does not authorize: implementation, reference changes, or treating every item
  below as one deliverable

## Purpose

RFC 0039 stays small enough to implement and validate as one foundation. This
proposal preserves the remaining C surface without making it part of that
foundation or implying that Hexal must reproduce every C feature.

An item advances only when a real library needs it and the design remains
smaller than a C wrapper implementing the same boundary.

## Capability inventory

| Capability | Problem it must solve | Primary design work | Initial direction |
| --- | --- | --- | --- |
| C function-pointer values | Store, pass, return, compare with Nil, and invoke C-ABI function addresses | ABI-qualified `Fun`, nullability, typed indirect calls, symbol addressability | Extend `Fun` only when the exact C ABI is representable |
| C callbacks into Hexal | Let C invoke a Hexal function later or from another thread | Lifetime, context pointer, retention, re-entry, Task/runtime attachment, panic/trap boundary | Noncapturing callbacks first; explicit context; no callback runs Hexal on an unattached foreign thread |
| Variadic C functions | Call APIs ending in `...` | C default promotions, admissible argument set, evaluation, format-contract checking | Prefer typed C wrappers; add direct calls only for demonstrated high-value APIs |
| Raw C unions | Pass or inspect untagged overlapping storage | Active-member responsibility, layout, field access, unsafe gating | Foreign-only nominal type; every member access unsafe |
| Bit-fields | Read and write implementation-defined packed fields | Width, signedness, storage unit, non-addressability, volatile access | Let the C compiler perform named field access; never reproduce offsets or masks |
| Flexible-array members | Work with a header followed by variable trailing storage | Allocation extent, element count, alignment, Slice construction, lifetime | Opaque record plus unsafe checked Slice accessor |
| C atomics | Interoperate with `_Atomic(T)` and `atomic_*` typedefs | ABI identity, memory order, lock-freedom, native Atomic incompatibility | Keep foreign atomics behind C wrappers unless a library requires direct access |
| Extended numeric types | Pass `long double`, `_BitInt`, complex, decimal, SIMD, and target extension values | Target representation, operations, constants, alignment, calling ABI | Pass/store a proven subset first; add Hexal arithmetic only from demand |
| Native error conventions | Translate `errno`, Windows last-error, status codes, and out-parameters | Capture timing, thread locality, portable categorization, owned messages | Binding or wrapper declares translation explicitly; never synthesize Error from a name alone |
| Non-default calling conventions | Use APIs such as 32-bit stdcall or target-specific vector conventions | Calling convention in function identity, pointer compatibility, target gating | Add only conventions required by qualified targets and supported by Zig/Clang |
| Dynamic libraries | Load a library and resolve symbols at runtime | Driver vs runtime ownership, library handle lifetime, typed symbol recovery, unload safety | Build-time linking remains default; dynamic lookup is explicit unsafe runtime work |
| Export Hexal functions to C | Produce a stable C-callable symbol and header | Supported ABI types, stable naming, visibility, initialization, foreign-thread entry | Non-generic free functions first; generate one C declaration header |

## Cross-cutting rules

- RFC 0039's binding-module boundary remains the compiler input. A child may
  extend its foreign declarations but does not make the compiler parse headers.
- Every capability is target-checked. Unsupported ABI facts fail before C
  generation.
- `unsafe` grants only the individually specified operation. It never disables
  ordinary typing, nullability, bounds, or exhaustiveness checks.
- The C compiler owns C layout and calling convention details. Hexal never
  derives offsets, masks, register classes, or promotions from its host.
- Foreign storage has no inferred ownership or automatic cleanup.
- C callbacks and dynamic symbols never silently acquire safe Hexal semantics.
- A wrapper written in C or Hexal is preferred when it avoids a general-purpose
  language feature with little reuse.
- Each accepted capability must preserve direct, readable generated C and add
  no runtime abstraction when the ABI operation itself is sufficient.

## Recommended decomposition

Do not implement this document as one RFC. Create focused child specifications
in this order only as concrete demand appears:

1. function-pointer values;
2. noncapturing callbacks with explicit context;
3. C exports;
4. native error-convention adapters;
5. one record-layout extension covering only the required union, bit-field, or
   flexible-array case;
6. one target-qualified calling-convention or numeric extension; and
7. dynamic loading or variadics only when a wrapper is demonstrably worse.

C atomics remain last unless a library ABI exposes them directly. Ordinary C
libraries should expose synchronization through functions instead of requiring
another language to share their atomic representation.

## Questions for future child RFCs

Every child must answer only the questions relevant to its capability:

- What concrete library and source program cannot be expressed through RFC
  0039 or a small wrapper?
- Is the value borrowed, retained, transferred, or program-lifetime?
- Can C invoke Hexal from an arbitrary native thread?
- Which exact target profiles and calling conventions qualify?
- Which operations require unsafe, and what facts does the programmer assert?
- Does the operation need new syntax, or can foreign-declaration metadata carry
  it?
- What generated C declaration or expression proves the ABI contract?
- Which submission, callback, unload, and shutdown path owns cleanup?
- Can failure map honestly to existing ErrorKind values, or must the foreign
  status remain explicit?

## Non-goals

- Implementing every C type or compiler extension for completeness.
- Treating C++ exceptions, `setjmp`/`longjmp`, signals, or process termination as
  Hexal Error.
- Automatic header parsing in the core compiler.
- A general ownership, lifetime, effect, or exception system.
- Making raw C unions behave like tagged Hexal unions.
- Assuming Hexal `Atomic<T>` has the ABI of C `_Atomic(T)`.

## Promotion rule

Before moving any item into active work:

1. verify the need against a concrete library;
2. create a new numbered child RFC with exact grammar and semantics;
3. give it exhaustive validation and a detailed implementation plan;
4. state its target and driver dependencies;
5. remove only that item from this inventory when the child closes; and
6. update `docs/reference.md` only after implementation stabilizes and the user
   explicitly approves the edit.

## Open questions

All capability-specific decisions remain open until their focused child RFC.
