package generator

import (
	"fmt"
	"slices"
	"strings"

	compilerTypes "hexal/compiler/types"
)

// tagRecord is one program-wide discriminant: a canonical union-member type
// or a canonical ADT variant. identity embeds the canonical keys so sorting
// by identity is sorting by stable canonical identity; name is the finalized
// collision-resolved constant.
type tagRecord struct {
	identity string
	name     string
}

// tagRegistry is the program-wide discriminant registry. It is finalized
// before hexal.h or any module pair renders, and every generated tag
// spelling flows through its lookups: a missing identity is an internal
// generation error, never a locally reconstructed name or ordinal.
type tagRegistry struct {
	records    []tagRecord
	byIdentity map[string]string
	seen       map[string]bool
	// failure records the first registry miss; lookups after it return a
	// stable placeholder so generation stays deterministic, and the error
	// surfaces when the phase settles.
	failure *compilerTypes.Diagnostic
}

func unionMemberIdentity(member compilerTypes.Type) string {
	return "type:" + member.CanonicalKey
}

func adtVariantIdentity(adt *compilerTypes.AdtType, index int) string {
	return "variant:" + compilerTypes.CanonicalNominalKey(adt.Name, adt.ModuleID) + ":" + adt.Variants[index].Name
}

// tagLabelBase derives a discriminant's readable C-suffix candidate from
// canonical Hexal identity: owner-qualified for nominal types, otherwise the
// sanitized display name. Compiler-owned builtins have no owner and keep the
// bare name.
func tagLabelBase(typ compilerTypes.Type) string {
	if typ.Object != nil {
		if typ.Object.Owner != "" {
			return typ.Object.Owner + "_" + compilerTypes.SanitizeIdentifier(typ.Object.Name)
		}
	}
	if typ.Adt != nil {
		if typ.Adt.Owner != "" {
			return typ.Adt.Owner + "_" + compilerTypes.SanitizeIdentifier(typ.Adt.Name)
		}
	}
	return compilerTypes.SanitizeIdentifier(typ.Name)
}

// buildTagRegistry discovers every reachable general-union member and
// concrete-ADT variant, deduplicates by identity, sorts by identity, and
// resolves label collisions in identity order: the first identity keeps the
// base, later ones append _0, _1, and so on.
func buildTagRegistry(unionOrders, adtOrders [][]compilerTypes.Type) *tagRegistry {
	registry := &tagRegistry{byIdentity: make(map[string]string), seen: make(map[string]bool)}
	for _, order := range unionOrders {
		for _, union := range order {
			members := compilerTypes.UnionMembers(union)
			for index := 0; index < members.Len(); index++ {
				member, _ := members.At(index)
				registry.add(unionMemberIdentity(member), tagLabelBase(member))
			}
		}
	}
	for _, order := range adtOrders {
		for _, adtType := range order {
			adt := adtType.Adt
			base := adt.Owner
			if base != "" {
				base += "_"
			}
			base += compilerTypes.SanitizeIdentifier(adt.Name) + "_"
			for index := range adt.Variants {
				registry.add(adtVariantIdentity(adt, index), base+compilerTypes.SanitizeIdentifier(adt.Variants[index].Name))
			}
		}
	}
	slices.SortStableFunc(registry.records, func(left, right tagRecord) int {
		return strings.Compare(left.identity, right.identity)
	})
	// Collision groups resolve by identity order regardless of final enum
	// position, so the resolved names depend only on the identity set.
	firstByBase := make(map[string]bool)
	for index := range registry.records {
		base := registry.records[index].name
		if !firstByBase[base] {
			firstByBase[base] = true
			registry.byIdentity[registry.records[index].identity] = base
			registry.records[index].name = base
			continue
		}
		for counter := 0; ; counter++ {
			candidate := fmt.Sprintf("%s_%d", base, counter)
			if !firstByBase[candidate] {
				firstByBase[candidate] = true
				registry.byIdentity[registry.records[index].identity] = candidate
				registry.records[index].name = candidate
				break
			}
		}
	}
	return registry
}

func (registry *tagRegistry) add(identity, base string) {
	if registry.seen[identity] {
		return
	}
	registry.seen[identity] = true
	registry.records = append(registry.records, tagRecord{identity: identity, name: base})
}

func (registry *tagRegistry) constant(identity string) string {
	name, ok := registry.byIdentity[identity]
	if !ok {
		if registry.failure == nil {
			diagnostic := compilerTypes.NewDiagnostic(compilerTypes.UnknownError, "generator", 0, 0,
				"generated discriminant is missing from the program-wide registry: "+identity)
			registry.failure = &diagnostic
		}
		// The placeholder keeps the current artifact renderable while the
		// run is already doomed; settled() discards every artifact.
		return "hex_tag_" + identity
	}
	return "hex_tag_" + name
}

// settled reports the first registry miss, if any. Callers check it once
// generation finishes and discard all artifacts on failure.
func (registry *tagRegistry) settled() error {
	if registry.failure == nil {
		return nil
	}
	return *registry.failure
}

// unionMemberTag resolves one canonical union-member type to its program-wide
// constant.
func (registry *tagRegistry) unionMemberTag(member compilerTypes.Type) string {
	return registry.constant(unionMemberIdentity(member))
}

// adtVariantTag resolves one canonical ADT variant to its program-wide
// constant.
func (registry *tagRegistry) adtVariantTag(adt *compilerTypes.AdtType, index int) string {
	return registry.constant(adtVariantIdentity(adt, index))
}

// unionPayloadField names a union's payload field for one canonical member:
// the unconditional hex_m_ prefix plus the member's assigned tag suffix, so
// the field label always matches its constant.
func (registry *tagRegistry) unionPayloadField(member compilerTypes.Type) string {
	return "hex_m_" + strings.TrimPrefix(registry.unionMemberTag(member), "hex_tag_")
}

// constantNames returns the finalized constants in enum order, spelled
// exactly as constant resolves references so the enum and its uses agree.
func (registry *tagRegistry) constantNames() []string {
	if registry == nil {
		return nil
	}
	names := make([]string, len(registry.records))
	for index, record := range registry.records {
		names[index] = "hex_tag_" + record.name
	}
	return names
}
