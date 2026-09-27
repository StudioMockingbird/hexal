# RFC 0223: Dict Correctness and Completeness Updates

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion
- Created: 2026-09-20
- Origin: Dict implementation review
- Coordinates with: RFC 0136 (prior key-type proposal), `docs/reference.md`

## Summary

The current `Dict<K, V>` contract in `docs/reference.md` is implemented and
works end to end, but a review found one stale diagnostic family, one
reference/code mismatch, one local-safety gap shared with `List`, several
deliberately missing operations, and a need to expand supported key types. This
RFC collects those findings so each can be fixed or explicitly declined in one
place.

The open-addressing representation, backward-shift deletion, versioned
iteration, and demand-driven component emission remain unchanged. Generalizing
the key contract is a proposed surface/semantics change; its protocol details
remain open below.

## Findings

### 1. Stale `.new` constructor spelling in diagnostics

The compiler-owned `.new()` constructor spelling was removed; the current form
is the call-shaped `Type(...)` / `Dict<K, V>(heap)`. Several diagnostics still
name `.new`:

- `compiler/checker/dicts.go:52`: `Dict<K, V>.new requires a Heap; got <T>`.
  Verified: `Dict<Int32, Int32>(h)` with a non-Heap `h` reports exactly that.
- `compiler/generator/arrays.go:189,192,392`: `Dict<K, V>.new has invalid
  checked metadata`, `Dict<K, V>.new result does not match its expected type`,
  `Dict<K, V>.new without a checked heap`.

The stale spelling is cross-cutting: `compiler/checker/lists.go:43`
(`List<T>.new requires a Heap`), `concurrency.go:186` (`Channel.new`),
`concurrency.go:285` (`Mutex.new`), and `concurrency.go:338` (`Atomic.new`) all
name removed constructors. `compiler/tests/integration/allocators_test.go:145`
asserts the old `List<T>.new requires a Heap` text, so a fix must update that
test. `compiler/checker/methods.go:468` already records that `.new()` is a
removed spelling, which confirms the diagnostics are the stale side.

### 2. Prior Strand hash finding is obsolete

The earlier Strand-specific concern no longer applies: Dict text keys are now
`String<N>`. The current contract hashes logical text bytes, and the generated
Dict delegates to the shared `hex_hash_text` helper. The key-expansion work must
preserve those cross-capacity text semantics; no Strand fix is part of this RFC.

### 3. Dict and List `free` have no same-binding freed-state tracking

`Heap.free(ptr)` and IO `close` mark freed state (`compiler/checker/alloc.go:258`,
`compiler/checker/io.go:329`). `Dict.free` and `List.free` do not: `List.free`
only records `releaseSource` so a memory stream outliving its source list is
rejected (`checker/lists.go:137`, `checker/io.go:64`). Verified by compiling and
running:

| Program | Result |
| --- | --- |
| `scores.free(h); scores.free(h)` | accepted |
| `scores.free(h); scores.get(1)` | accepted |
| `scores.free(h); scores.insert(1, 2)` | accepted |
| `values.free(h); values.free(h)` (List) | accepted |

The runtime then frees the header/buckets twice. `docs/reference.md:385-391`
documents the aliasing narrowing ("freeing one alias leaves others dangling ...
misuse may produce undefined behavior"), but a second free or use through the
same binding is locally decidable and inconsistent with the tracked `Heap`
path. This proposal tracks only that binding; copied handles can still dangle
and remain the programmer's responsibility. The integration test
`compiler/tests/integration/dict_test.go` (`TestDictShallowCopySemantics`)
currently treats the same-binding double-free as valid, so the decision below
changes that test.

### 4. Missing operations

- No `clear`. `List` has `clear` (`checker/lists.go:77,91`); `Dict` has no way
  to drop all entries except `free` plus reconstruction.
- No `keys`/`values`/`entries` views. Only iteration exposes contents.

Neither is in the current contract; both are usability gaps, not violations.

### 5. Expand keys to all scalar and inline-text types

The current reference permits only `Int32` and `String<N>` keys. The selected
direction is to permit every scalar key type plus every inline `String<N>`:

| Key type | Current support | Proposed support |
| --- | --- | --- |
| `Bool` | no | yes |
| `UInt8`, `UInt16`, `UInt32`, `UInt64` | no | yes |
| `Int8`, `Int16`, `Int32`, `Int64` | `Int32` only | yes |
| `Size` | no | yes |
| `Byte` | alias of `UInt8`; no | yes, same key identity as `UInt8` |
| `Rune` | no | yes |
| `Float32`, `Float64` | no | yes, subject to the float-key equality decision below |
| `EoS` | no | yes |
| `String<N>` | yes | yes, compare/hash logical bytes independent of capacity |
| heap `String` | no | no; it is a handle to separately owned bytes |
| user-defined object, ADT, union, collection, pointer | no | no in this scope |

`Nil` is not a standalone value type and is not a key candidate. `Unknown` and
foreign records are not scalar value types and remain ineligible. `Byte` is a
transparent alias, not a distinct key type from `UInt8`.

Integer-like keys use their value equality and must hash equal values equally.
Inline text keys compare and hash exactly their logical bytes, including
embedded zero bytes where representable, and compare equal across capacities.
No user-defined hash/equality protocol or operator overloading is introduced by
this scope.

Float keys follow IEEE equality for accepted values: positive and negative zero
compare as the same key and must produce the same hash. NaN is rejected at
runtime by every key-taking Dict operation (`insert`, `get`, `find`, `contains`,
and `remove`), since NaN is unequal to itself and would otherwise be an
unfindable key. The runtime diagnostic text is specified with the implementation
plan.

### 6. xxHash dependency and Dict hashing

Use the official xxHash `v0.8.4` release, pinned as a source submodule at its
resolved commit, and use `XXH3_64bits()` with its default zero seed for Dict key
hashes. Hashing is not intended to resist adversarial collision attacks; secure
or keyed hashing is explicitly out of scope.

Hash input is canonical for each key family: `String<N>` hashes only its
logical bytes, independent of capacity and unused storage; `Bool` hashes one
canonical byte; integer, `Size`, and `Rune` keys hash their fixed-width value
encoding; `Float32` and `Float64` hash their IEEE value encoding after mapping
both signed zeros to positive zero; `EoS` hashes an empty input. NaN is rejected
before hashing. Equality remains the final key identity check, so ordinary hash
collisions do not merge distinct keys.

xxHash is a demand-driven native runtime dependency of Dict. The implementation
adds the pinned source under `modules/xxhash`, records its build inputs, and
ships the required static archive, public header, and license in each shipped
target runtime pack. A generated program that uses Dict links xxHash; a program
that does not use Dict does not materialize or link it. The archive build and
pack manifests are part of the same implementation as the compiler and
generated-C changes, not a follow-up task.

## Recommended scope

1. Replace the stale `.new` spelling in the Dict, List, Channel, Mutex, and
   Atomic diagnostics with the current call-shaped spelling, and update
   `allocators_test.go`.
2. Preserve content-based inline-text equality and hashing across capacities.
3. Track freed state for Dict and List bindings as `Heap.free` does. A second
   `free` or any use after `free` through the same binding is rejected. Copies
   remain outside this local guarantee and may dangle after another alias is
   freed.
4. Add `Dict.clear()`. It sets the logical length to zero, retains the allocated
   bucket capacity, and increments the structural version even when already
   empty, matching `List.clear()` invalidation behavior. It does not free
   referents held by entries.
5. Expand the accepted key set to the scalar types and inline `String<N>` listed
   above. Keep heap `String` and user-defined key types excluded. Reject NaN for
   every key-taking Dict operation; positive and negative zero compare and hash
   as the same key. Use xxHash v0.8.4 `XXH3_64bits()` with the canonical key
   encodings defined above, and integrate it as a demand-driven static runtime
   dependency in the same implementation.

## Validation

1. `Dict<Int32, Int32>(h)` with a non-Heap argument reports a message that names
   the current spelling, and no diagnostic anywhere in the compiler names a
   removed `.new()` constructor. `allocators_test.go` asserts the new text.
2. Inline-text hashing remains based on logical bytes, and the existing
   `TestDictStringKeys` coverage passes.
3. A second `free` on a Dict or List binding, or a `get`/`insert`/`remove`/
   `length`/iteration through that same binding after free, is rejected with a
   diagnostic. Copies retain the documented dangling-alias limitation; do not
   claim that same-binding tracking validates copied aliases. Update the
   same-binding cases in `TestDictShallowCopySemantics`.
4. `Dict.clear()` makes `length()` zero, a subsequent `insert` works, and
   iteration after clearing yields nothing. Clearing retains capacity and
   invalidates a traversal active when clear executes; entry referents are not
   freed.
5. `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass; the
   tagged C23 suite passes; the workbench manifest is rebuilt only if generated
   C changes, and any change is reviewed.
6. Cover each proposed scalar key type and inline text keys at different
   capacities. Equal values must identify the same entry for insertion,
   lookup, contains, and removal. For both Float32 and Float64, verify that
   inserting one signed zero and querying/removing with the other addresses the
   same entry, and that NaN traps for insert, get, find, contains, and remove.
7. Every shipped target runtime pack contains the pinned xxHash v0.8.4 archive,
   header, license, build record, and verified manifest hashes. Dict output
   declares the xxHash runtime dependency and compiles, links, and runs against
   the archive on each shipped target; non-Dict output has no xxHash dependency.

## Non-goals

- Replacing open addressing or backward-shift deletion.
- Adding user-defined hash/equality key protocols or operator overloading.
- Adding secure or keyed hashing for adversarial input.
- Adding structural hashing for aggregate keys.
- Changing Dict iteration order guarantees.
- Changing the aliasing narrowing for copied handles.

## Open questions

1. Are `keys`/`values`/`entries` views wanted, or is pair iteration sufficient?
