# RFC 0223: Dict Correctness and Completeness Updates

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion
- Created: 2026-09-20
- Origin: Dict implementation review
- Coordinates with: RFC 0136 (expanded key types), `docs/reference.md`

## Summary

The `Dict<K, V>` contract in `docs/reference.md` is implemented and works end to
end, but a review found one stale diagnostic family, one reference/code
mismatch, one local-safety gap shared with `List`, and several deliberately
missing operations. This RFC collects those findings so each can be fixed or
explicitly declined in one place.

Nothing here is a redesign. The open-addressing representation, backward-shift
deletion, versioned iteration, and demand-driven component emission are correct
and stay.

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

### 2. Strand hash description does not match the code

`docs/reference.md` states the Strand hash excludes the terminator/zero tail.
`compiler/generator/packages/dict.h:15-22` (`hex_hash_Strand`) hashes all 32
bytes of `key.data`. Because Strand is zero-filled and NUL-free by construction,
equal Strands have identical 32-byte representations, so hashing the tail is
correct for equality but does not match the stated algorithm. Either the
reference or the hash must change; hashing only the logical length would also
avoid touching the tail.

### 3. Collection `free` has no local freed-state tracking

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
misuse may produce undefined behavior"), but a same-binding double-free is
locally decidable and is inconsistent with the tracked `Heap` path. The
integration test `compiler/tests/integration/dict_test.go` (`TestDictShallowCopySemantics`)
currently treats the double-free as valid, so the decision below changes a test.

### 4. Missing operations

- No `clear`. `List` has `clear` (`checker/lists.go:77,91`); `Dict` has no way
  to drop all entries except `free` plus reconstruction.
- No `keys`/`values`/`entries` views. Only iteration exposes contents.

Neither is in the current contract; both are usability gaps, not violations.

### 5. Deferred key types

Keys are exactly `Int32` or `Strand` (`compiler/types/collections.go:464-468`).
Bool, other fixed-width integers, `Size`, and `Rune` remain deferred to RFC 0136,
which is still Open Discussion. This RFC does not duplicate that work.

## Recommended scope

1. Replace the stale `.new` spelling in the Dict, List, Channel, Mutex, and
   Atomic diagnostics with the current call-shaped spelling, and update
   `allocators_test.go`.
2. Reconcile the Strand hash with the reference: either hash only the logical
   payload (and keep the reference), or correct the reference to state that the
   zero tail is included.
3. Decide collection `free` freed-state tracking. Recommended: track the
   handle binding the way `Heap.free` does, so a second `free` or a use after
   `free` on the same binding is rejected with a diagnostic; keep the existing
   aliasing narrowing for copies.
4. Add `Dict.clear()` if the usability gap is worth the surface; otherwise
   record the omission as deliberate.

## Validation

1. `Dict<Int32, Int32>(h)` with a non-Heap argument reports a message that names
   the current spelling, and no diagnostic anywhere in the compiler names a
   removed `.new()` constructor. `allocators_test.go` asserts the new text.
2. The Strand hash and `docs/reference.md` agree, and `TestDictStrandKeys`
   still passes byte-for-byte.
3. If freed-state tracking is added: a second `free` on the same binding, a
   `get`/`insert`/`remove`/`length`/iteration after `free`, and the same through
   a copy are each rejected with a diagnostic; the existing
   `TestDictShallowCopySemantics` cases are updated to match.
4. If `clear` is added: `clear` empties the dict, `length()` is zero after it,
   a subsequent `insert` works, and iteration over a cleared dict yields
   nothing.
5. `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass; the
   tagged C23 suite passes; the workbench manifest is rebuilt only if generated
   C changes, and any change is reviewed.

## Non-goals

- Redesigning the Dict representation or its hashing algorithm.
- Expanding key types (RFC 0136).
- Adding structural hashing for aggregate keys.
- Changing Dict iteration order guarantees.
- Changing the aliasing narrowing for copied handles.

## Open questions

1. Should freed-state tracking cover every `free`-exposing handle (List, Dict,
   Channel, Mutex, Pool), or only Dict and List? The `Heap` path is the model.
2. Is `clear` wanted, and does it release buckets (reset to capacity 0) or only
   deactivate entries (keep capacity)?
3. Should the Strand hash stop at the logical length for speed, or is hashing
   the fixed 32 bytes preferred for a branch-free loop?
4. Are `keys`/`values`/`entries` views wanted, or is iteration sufficient?
