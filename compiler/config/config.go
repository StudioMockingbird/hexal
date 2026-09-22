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

// RuntimeABIVersion is the generated/runtime ABI that checked-in runtime packs
// must match. Generated C and the runtime components agree on this version; an
// incompatible generated-runtime change increments it and requires refreshed
// target packs in the same change. The driver compares a pack manifest's
// runtime_abi_version with this value, and no user setting can override it.
const RuntimeABIVersion uint32 = 1
