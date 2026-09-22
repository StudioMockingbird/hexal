// Package specdata is the compiler's fact registry: the facts about runtime
// components, target semantics, type constructors, and built-in methods that
// more than one phase must agree on, held as records rather than as code.
//
// Two boundaries define it. It imports no compiler package, because
// compiler/corelib already imports compiler/types and stores live Type values,
// so importing types here would close a cycle: identifiers cross this boundary
// and Type values do not. It declares no driver fact -- no triple, archive
// path, qualification flag, SDK, or sysroot -- because qualification depends on
// the installed backend, pack availability, and host state, none of which the
// compiler may observe.
//
// Records describe type constructors, never specializations: String<N> makes a
// per-specialization registry impossible because N is unbounded, so a C name is
// derived from a record's arguments rather than written down beside it.
//
// Records hold facts, not behavior. Demand stays in an explicit Go function at
// the site that needs it; a func-typed record field would be the predicate
// language this package deliberately does not invent.
//
// Registry validity is a property of the source tree, so Validate is called
// from a test and never from init: a maintainer's mistake must fail the suite,
// not panic every consumer of the compiler.
package specdata

// Validate reports the first internal inconsistency in the registry: a record
// whose identifier is empty or repeated, a reference to an identifier no record
// declares, or a domain that names the same fact twice.
//
// It is called from a test rather than from init, so a bad record fails the
// suite instead of crashing every consumer. An empty registry is valid, which
// is what lets each domain arrive in its own slice without a placeholder.
//
// Each domain owns its check and reports the first failure; adding a domain
// means adding one delegated call, not editing the others.
func Validate() error {
	if err := validateTargets(); err != nil {
		return err
	}
	return nil
}
