# RFC 0045: Rename the project from Seawitch to Hexal

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-12
- Features: project-wide identity rename; `sw_` private C namespace becomes
  `hex_`; live documentation and workbench branding updated
- Created: 2026-08-12
- Coordinates with: RFC 0004 (source and generated identifiers, archived)

## Summary

The project has been copied from its former home and renamed "hexal" at the
filesystem, module, and binary level (`go.mod` says `module hexal`, binaries
are `hexal-workbench.exe`), but the word "Seawitch" and the Seawitch-owned
private C namespace prefix `sw_` still appear throughout compiler source,
generated-C strings, live documentation, and the workbench UI.

This RFC removes every remaining Seawitch reference from live code and live
documentation and renames the `sw_` prefix to `hex_`. It leaves all closed
specs untouched.

## Scope decisions

Three decisions fix the boundary of this rename:

1. **Closed specs are immutable and stay untouched.** AGENTS.md declares that a
   closed spec is immutable, and this RFC does not override that. Every file
   under `docs/specs/` (including `docs/specs/archive/`) keeps its `Seawitch`
   prose, its ```seawitch code fences, and its `sw_` spellings forever. This
   means the word "Seawitch" and the `sw_` prefix will permanently remain in
   the repository inside those files; that is accepted. Spec references in
   code comments (for example `// RFC 0041 (ADR): Seawitch has no
   module-global ...`) are live comments and are renamed.
2. **The private C namespace prefix `sw_` becomes `hex_`** (for "hexal").
   Every emitted C name `sw_X` becomes `hex_X`, and the uppercase guard and
   constant macros `SW_*` (`SW_HEAP_DEFAULT`, `SW_FILE_READ`, `SW_TASK_READY`,
   `SW_NUMERIC_TRAP_DEFINED`, and so on) become `HEX_*`. No other identifier
   structure changes.
3. **Markdown code-fence language tags ```seawitch become ```hexal** in live
   docs. Closed specs keep ```seawitch.

## Word rename: "Seawitch"/"seawitch" to "Hexal"/"hexal"

Use the proper noun `Hexal` in prose, titles, headings, and human-facing
messages. Use lowercase `hexal` in code-fence tags and lowercase identifier
contexts.

The word is renamed in:

- `AGENTS.md` — title and all prose mentions.
- `docs/grammar.md` — title; "remain valid Seawitch identifiers."
- `docs/language.md` — title; every prose mention, including the identifier
  and naming sections.
- `docs/status.md` — title.
- `compiler/**` Go source — package doc comments, file header comments, and
  inline comments in `lexer`, `parser`, `checker`, `types`, and `generator`.
- Human-facing strings the generator embeds in generated C:
  - `#error "Seawitch Task runtime requires C23 threads ..."` in
    `compiler/generator/concurrency.go`;
  - the `// Seawitch entry point.` comment in `compiler/generator/concurrency.go`;
  - every `static_assert(...)` and `#error` diagnostic whose text begins
    `"Seawitch requires ..."` in `compiler/generator/generator.go`.
- `workbench/index.html` — the `<title>` and the `<h1>` both read "Hexal
  Workbench".
- Test data string literals. `"Seawitch"` used as a `Strand` or `String`
  value in tests becomes `"hexal"`, and every dependent assertion is updated
  to match: a length of 8 becomes 5, `label[0] == 83` becomes `label[0] ==
  104`, and so on. See `compiler/c23_text_smoke_test.go` and
  `compiler/text_conformance_test.go`.

## Code-fence rename

In `docs/grammar.md`, `docs/language.md`, and `docs/status.md`, every
```` ```seawitch ```` opening fence becomes ```` ```hexal ````. This changes
only the fence tag; fence content is untouched. `docs/specs/**` fences are
not changed.

## Prefix rename: `sw_` to `hex_`

The prefix is mechanical: every emitted identifier `sw_X` becomes `hex_X`,
including helper functions, typedefs, struct tags, enum tags, temporaries,
captures, and literal storage names. The sources of truth that must change
first are:

- `compiler/generator/generator.go` — `PrivateCName` (`sw_v_`, `sw_m_`,
  `sw_f_`) and the `sw_return_%d` temporaries.
- `compiler/types/types.go` — builtin CNames `sw_heap`, `sw_string`,
  `sw_strand`, `sw_eos`, `sw_mutex`, `sw_rune_cursor`, `sw_file_mode`,
  `sw_file`, `sw_t_Error`, and the nominal-type CName pattern `sw_t_`.
- `compiler/types/collections.go` — `sw_array_`, `sw_view_`, `sw_list_`,
  `sw_stream_`, `sw_task_`, `sw_channel_`, `sw_atomic_`, `sw_dict_`.
- `compiler/types/adt.go` — the `"sw_" + SanitizeIdentifier(name)` variant
  and tag names.
- `compiler/types/unions.go` — `sw_internal_union_%d`.
- The uppercase `SW_` guard and constant macros in
  `compiler/generator/*.go` and `compiler/checker/io.go` (`SW_HEAP_DEFAULT`,
  `SW_FILE_READ`/`SW_FILE_WRITE`/`SW_FILE_APPEND`, `SW_TASK_*`,
  `SW_NUMERIC_TRAP_DEFINED`), which become `HEX_*`.
- Every remaining literal `sw_` string in `compiler/generator/*.go`, covering
  the runtime helper families: `sw_heap_*`, `sw_array_at_*`,
  `sw_view_*`, `sw_list_*`, `sw_dict_*`, `sw_stream_*`, `sw_chan_*`,
  `sw_mutex_*`, `sw_task_*`, `sw_atomic_*`, `sw_string_*`, `sw_strand_*`,
  `sw_rune_cursor_*`, `sw_file_*`, `sw_sched_error`, `sw_equal_*`,
  `sw_shl_`/`sw_shr_`, `sw_bitcast_*`, `sw_convert_*`, `sw_div_*`/`sw_rem_*`,
  `sw_to_*_bytes`/`sw_from_*_bytes`, `sw_lit_*`, `sw_for_*`, `sw_try_*`,
  `sw_match_*`, `sw_defer_capture_*`, `sw_spawn_*`, and `sw_stream_produce_*`.
- All `strings.TrimPrefix(..., "sw_")` calls in `compiler/generator/*.go`,
  which must trim the new prefix.
- Tests that assert generated C: `compiler/*_test.go` and
  `compiler/generator/*_test.go`.
- The temporary I/O filename `sw_io_target.txt` in
  `compiler/c23_io_smoke_test.go` becomes `hex_io_target.txt`.
- Live documentation that specifies the convention:
  - `docs/language.md` — the private-name tables (for example `score` to
    `hex_v_score`), the `sw_v_int`/`sw_m_`/`sw_t_`/`sw_heap_*` examples, and
    the generated-C samples;
  - `docs/status.md` — the `sw_string`, `sw_strand`, and
    `sw_div_*`/`sw_rem_*` mentions.

`docs/specs/**` is not changed.

## Non-goals

- No change to any file under `docs/specs/` or `docs/specs/archive/`.
- No language, semantic, or ABI change: only the identifier prefix and prose
  differ.
- No change to `go.mod` (module path is already `hexal`) or to binary,
  directory, or package names.
- No rename of source identifiers other than the `sw_` prefix.

## Verification

1. `go test ./...` passes.
2. A repository scan finds no occurrence of `(?i)seawitch`, no `sw_`, and no
   `SW_` anywhere outside `docs/specs/`.
3. The c23 build-tagged tests pass with gcc or clang installed.
4. The workbench is rebuilt into `bin/`, restarted, and its UI shows "Hexal
   Workbench".

## Acceptance criteria

This RFC is implemented when:

1. The word `seawitch` (any case) appears nowhere except under `docs/specs/`.
2. The prefixes `sw_` and `SW_` appear nowhere except under `docs/specs/`.
3. Every ```seawitch code fence in live docs is ```hexal.
4. Generated C emits only `hex_`-prefixed private names; `sw_` is never
   emitted.
5. `PrivateCName`, all builtin CNames, and all collection/ADT/union CNames use
   the `hex_` prefix and the two agree with `docs/language.md`.
6. All Go tests pass, including the c23 build-tagged smoke tests.
7. The workbench builds, runs, and displays Hexal branding.
8. Every file under `docs/specs/` is byte-identical to its state before this
   RFC.

## Readiness

This is a mechanical rename. All scope decisions are fixed (closed specs
untouched, `sw_` to `hex_`, fences to ```hexal). No design question remains
open. Ready for Implementation.
