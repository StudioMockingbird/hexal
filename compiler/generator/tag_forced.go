package generator

import "hexal/compiler/types"

// forcedTagMembers lists union members named only by std/json and std/regex
// adapters, including Nil for the nullable capture-list element. Source
// reachability does not necessarily discover those identities, but generated
// units still need their program-wide discriminants.
func forcedTagMembers(merged *programEmission) []types.Type {
	if merged == nil {
		return nil
	}
	jsonUsed := merged.jsonState != nil && merged.jsonState.used
	regexUsed := merged.regexState != nil && merged.regexState.used
	if !jsonUsed && !regexUsed {
		return nil
	}
	forced := make([]types.Type, 0, 4)
	if regexUsed {
		forced = append(forced,
			types.RegexPatternType(),
			types.RegexSpanType(),
			types.RegexMatchType(),
			types.Nil,
		)
	}
	if jsonUsed {
		forced = append(forced, types.JsonValueType())
	}
	return forced
}

var _ = types.IsError
