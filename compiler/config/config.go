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

// MaxSyntaxDepth bounds recursive-syntax nesting. It is a compiler limit, not
// a language rule: exceeding it reports the same limit the lexer's
// MaxInterpolationDepth reports, with the same message shape.
const MaxSyntaxDepth = 128

// MaxInterpolationDepth bounds nested interpreted-string-with-interpolation
// parsing. It is separate from MaxSyntaxDepth because an interpolation nests
// without a syntax node, but it reports the same limit value so both stages
// agree.
const MaxInterpolationDepth = 128

// ForeignInspectionByteLimit bounds the captured output of one foreign
// inspection process; output past it is discarded rather than buffered.
const ForeignInspectionByteLimit = 64 << 20

// ForeignInspectionTimeout bounds one foreign inspection process end to end.
var ForeignInspectionTimeout = 30 * time.Second

// DefaultTaskStackReserveBytes and DefaultTaskStackCommitBytes are the per-Task
// stack sizes a zero Project selects: 1 MiB of address space and 8 KiB
// committed at spawn. The POSIX backend lazily maps the whole reserve and never
// pre-commits; the Windows backend passes both to CreateFiberEx.
const (
	DefaultTaskStackReserveBytes = 1 << 20
	DefaultTaskStackCommitBytes  = 8 << 10
)

// MaxInlineStringCapacity is the largest String<N> capacity: one page, past
// which an inline value stops being cheaper than a heap allocation. It is the
// bound the canonicality rule, the checker's capacity diagnostic, and the
// reference's capacity table all read.
const MaxInlineStringCapacity = 4096

// ErrorHeaderCapacity and ErrorMessageCapacity are the two capacities Error
// fixes, in bytes: an ErrorKind.Other header and an Error message. The types
// built from them stay in compiler/types, because they are type identity; only
// the values are configuration.
const (
	ErrorHeaderCapacity  = 128
	ErrorMessageCapacity = 256
)
