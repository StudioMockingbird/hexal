package generator

import compilerTypes "hexal/compiler/types"

// elementNeedsJson reports whether one collection element's record is owned
// by hexal/json.h: the Value union and the ordered Member record are the JSON
// data-model types a component specialization spells directly.
func elementNeedsJson(element compilerTypes.Type) bool {
	return compilerTypes.IsJsonValue(element) || compilerTypes.IsJsonMember(element)
}
