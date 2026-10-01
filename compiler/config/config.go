// Package config holds the compiler's tunable policy: the resource limits,
// defaults, and C-layout capacities that more than one phase must agree on.
//
// Membership has one rule. A declaration belongs here only when it is tunable
// policy. Type identity stays in compiler/types, diagnostic wording stays with
// the phase that emits it, and target facts stay in the target profiles, so
// this package never becomes a second home for any of them.
//
// It imports only the Go standard library, so every phase -- lexer, parser,
// checker, generator, compiler, and driver -- can depend on it without a cycle.
package config

import "time"

// RuntimeABIVersion is the generated/runtime ABI that checked-in runtime packs
// must match. Generated C and the runtime components agree on this version; an
// incompatible generated-runtime change increments it and requires refreshed
// target packs in the same change. The driver compares a pack manifest's
// runtime_abi_version with this value, and no user setting can override it.
const RuntimeABIVersion uint32 = 1

// PageSizeBytes is the page size the POSIX task-stack guard depends on.
// Windows rounds its own arguments, but the rule applies to both targets, so a
// value is never accepted on one and rejected on the other.
const PageSizeBytes uint64 = 4096

// MaxSyntaxDepth caps source-controlled recursive descent at 128 levels. The
// bound leaves parser call-stack headroom and turns deeper input into a
// diagnostic; it is a safety limit, not a measured threshold or language rule.
const MaxSyntaxDepth = 128

// MaxInterpolationDepth separately caps recursive interpolation lexing at the
// parser's syntax-depth limit. Interpolation nests without a syntax node, so
// the lexer needs its own guard while both stages report the same limit.
const MaxInterpolationDepth = 128

// ForeignInspectionByteLimit bounds each captured output stream from one
// foreign inspection process; excess output is discarded. This conservative
// resource ceiling is not a measured buffer-size optimum.
const ForeignInspectionByteLimit = 64 << 20

// MaxStructuralPrintDepth caps how many structural aggregates one printed
// value may enter before the generated helper prints the unquoted marker
// "..." and returns. Cyclic values and adversarially deep values would
// otherwise exhaust the native C stack inside print helpers; ordinary
// diagnostic output stays well below this. It is a conservative safety
// ceiling chosen against that exhaustion mode, not a measured optimum, and
// JSON serialization deliberately stays outside it: value.stringify either
// emits complete output or fails.
const MaxStructuralPrintDepth = 16

// ForeignInspectionTimeout caps one foreign inspection process end to end.
// It is a conservative external-process resource ceiling, not a performance
// target.
var ForeignInspectionTimeout = 30 * time.Second

// DefaultTaskStackReserveBytes and DefaultTaskStackCommitBytes are the per-Task
// stack sizes a zero Project selects: 1 MiB of address space and 8 KiB
// committed at spawn. The POSIX backend lazily maps the whole reserve and never
// pre-commits; the Windows backend passes both to CreateFiberEx.
const (
	DefaultTaskStackReserveBytes = 1 << 20
	DefaultTaskStackCommitBytes  = 8 << 10
)

// MaxInlineStringCapacity caps payload bytes stored inline in String<N>.
// Because each value carries a length and N payload bytes by value, the cap
// also bounds its contribution to enclosing objects and stack layouts; it is
// not a measured heap-allocation crossover.
const MaxInlineStringCapacity = 4096

// MaxInlineListEstimatedBytes is a conservative ceiling for one inline List value.
const MaxInlineListEstimatedBytes uint64 = 64 * 1024

// ErrorHeaderCapacity and ErrorMessageCapacity are the two capacities Error
// fixes, in bytes: an ErrorKind.Other header and an Error message. The types
// built from them stay in compiler/types, because they are type identity; only
// the values are configuration.
const (
	ErrorHeaderCapacity  = 128
	ErrorMessageCapacity = 256
)

// RegexMaxPatternBytes bounds the source pattern's UTF-8 bytes at compilation
// time (pcre2_set_max_pattern_length). A regular expression approaching 64 KiB
// is already outside ordinary program use; the bound prevents
// attacker-controlled input from requesting unbounded compiler work.
const RegexMaxPatternBytes = 64 << 10

// RegexMaxCompiledPatternBytes bounds the compiled representation
// (pcre2_set_max_pattern_compiled_length). The 8-bit library's default
// two-byte internal link size already limits compiled patterns to
// approximately this size; Hexal retains the smaller and faster representation
// instead of widening links for extreme patterns.
const RegexMaxCompiledPatternBytes = 64 << 10

// RegexMaxParenthesisDepth pins PCRE2's documented default parenthesis nesting
// limit (pcre2_set_parens_nest_limit), which exists to protect the system
// stack during compilation.
const RegexMaxParenthesisDepth = 250

// RegexMatchLimit is the per-match work allowance (pcre2_set_match_limit),
// PCRE2's established default rather than a lowered Hexal choice.
const RegexMatchLimit = 10_000_000

// RegexMatchDepthLimit is the per-match backtracking nesting limit
// (pcre2_set_depth_limit), a finite value replacing a default that is
// effectively unlimited for Hexal's purposes. It is a conservative safety
// ceiling, not a measured optimum.
const RegexMatchDepthLimit = 10_000

// RegexMatchHeapLimitKiB is the per-match heap allowance in KiB
// (pcre2_set_heap_limit), a finite value replacing a default that is
// effectively unlimited for Hexal's purposes. It is a conservative safety
// ceiling, not a measured optimum.
const RegexMatchHeapLimitKiB = 8 << 10

// JSONMaxDepth is the JSON traversal depth compiler-owned bound: the reader
// and writer depth macros are set from it when the yyjson archive is built,
// and Hexal's translation, stringify, and cleanup guards consult it. It is a
// safety ceiling against adversarial nesting, not a language argument or
// per-call option.
const JSONMaxDepth = 256
