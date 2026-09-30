package generator

import (
	"hexal/compiler/checker"
	"hexal/compiler/config"
	"hexal/compiler/corelib"
	compilerTypes "hexal/compiler/types"
)

// std/json generation: the shared raw declarations (json.h), the Hexal-tree
// helpers (json_value.c: build and the visited-release traversal), and the
// yyjson adapter (json_adapter.c, the only translation unit including
// <yyjson.h>). Demand reads the CorelibCallExpression nodes the checker
// resolved through the std/json module table: free alone selects the helper
// unit and no yyjson payload; parse or stringify also select the adapter.

// generatedJSONState records one program's std/json reachability.
type generatedJSONState struct {
	used      bool
	parse     bool
	stringify bool
	free      bool
	adapters  []corelibAdapter
	// fileLiteral is the module source key's Error file literal, interned
	// only when an adapter exists to build a failure from it.
	fileLiteral literalHandle
}

// discoverGeneratedJSON walks one module for std/json module calls.
func discoverGeneratedJSON(program checker.Program, logicalKey string, literals *literalRegistry) *generatedJSONState {
	state := &generatedJSONState{}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.CorelibCallExpression {
				return nil
			}
			path, _, ok := corelib.FunctionByRuntime(node.Name)
			if !ok {
				return unknownExpressionDiagnostic()
			}
			if path != "std/json" {
				return nil
			}
			state.used = true
			switch node.Name {
			case "hex_json_parse":
				state.parse = true
			case "hex_json_stringify":
				state.stringify = true
			case "hex_json_free":
				state.free = true
			default:
				return unknownExpressionDiagnostic()
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return state
	}
	state.adapters = discoverModuleAddonAdapters(program, "std/json")
	if len(state.adapters) > 0 {
		state.fileLiteral = literals.Intern(logicalKey)
	}
	return state
}

// jsonComponents returns the std/json component artifacts the reachable
// operations demand. The value helper unit exists for any reachable tree;
// the adapter exists exactly when parse or stringify is reachable, and no
// other artifact exists.
func jsonComponents(merged *programEmission) ([]componentArtifact, error) {
	state := merged.jsonState
	if state == nil || !state.used {
		return nil, nil
	}
	adapter := state.parse || state.stringify
	model := jsonSourceModel{Adapter: adapter, Depth: config.JSONMaxDepth}
	// The ADT tags every spelling renders from the same program-wide tag
	// registry every module-owned ADT uses: json_value.c's free walk switches
	// on them whether or not the yyjson adapter is selected, so the discovery
	// pass must have registered Value for every reachable tree.
	tags := merged.tags
	adt := compilerTypes.JsonValueType().Adt
	model.TagNull = tags.adtVariantTag(adt, 0)
	model.TagBool = tags.adtVariantTag(adt, 1)
	model.TagInt = tags.adtVariantTag(adt, 2)
	model.TagUInt = tags.adtVariantTag(adt, 3)
	model.TagFloat = tags.adtVariantTag(adt, 4)
	model.TagText = tags.adtVariantTag(adt, 5)
	model.TagArray = tags.adtVariantTag(adt, 6)
	model.TagObject = tags.adtVariantTag(adt, 7)
	return []componentArtifact{
		{key: "hexal/json.h", template: "json.h", model: model},
		{key: "hexal/json_value.c", template: "json_value.c", model: model},
		{key: "hexal/json_adapter.c", template: "json_adapter.c", model: model},
	}, nil
}

// jsonSourceModel gates the letters the adapter unit spells and the raw
// adapter's file spelling.
type jsonSourceModel struct {
	Adapter   bool
	Depth     int
	TagNull   string
	TagBool   string
	TagInt    string
	TagUInt   string
	TagFloat  string
	TagText   string
	TagArray  string
	TagObject string
}

// mergeJSONInto unions one module's JSON demand into the program state.
func mergeJSONInto(merged, state *generatedJSONState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.parse = merged.parse || state.parse
	merged.stringify = merged.stringify || state.stringify
	merged.free = merged.free || state.free
}

// moduleJSONComponent selects the json component header one module's checker
// reaches.
func moduleJSONComponent(emission *moduleEmission) []string {
	if emission.jsonState == nil || !emission.jsonState.used {
		return nil
	}
	return []string{"hexal/json.h"}
}

var _ = compilerTypes.IsJsonValue
