package generator

import "hexal/compiler/corelib"

// corelibRawResultName spells one program/entropy runtime result's raw record
// shape, keyed from the record the component header carries; an empty result
// means a result shape with no program/entropy record, which fails closed in
// the writer. The std/json and std/regex records are declared by their own
// families through addonRawResultName because one shared shape (ResultString)
// names a different record in each.
func corelibRawResultName(result corelib.Result) string {
	switch result {
	case corelib.ResultString:
		return "hex_program_string_result"
	case corelib.ResultStringSlice:
		return "hex_program_arguments_result"
	case corelib.ResultNil:
		return "hex_entropy_fill_result"
	default:
		// Size and NoValue carry no record: one is a direct call, the other
		// produces no value at all.
		return ""
	}
}
