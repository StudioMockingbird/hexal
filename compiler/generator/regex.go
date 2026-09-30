package generator

import (
	"hexal/compiler/checker"
	"hexal/compiler/config"
	"hexal/compiler/corelib"
	compilerTypes "hexal/compiler/types"
)

// std/regex generation: the shared raw declarations (regex.h) and the PCRE2
// adapter pair (regex.c, the only translation unit including <pcre2.h> and
// defining PCRE2_CODE_UNIT_WIDTH 8). Demand reads the CorelibCallExpression
// nodes the checker resolved through the std/regex module table: naming only
// Span, Match, or Pattern, and free_match alone, selects no dependency,
// because it releases only Hexal's own capture List.

type generatedRegexState struct {
	used      bool
	compile   bool
	test      bool
	find      bool
	capture   bool
	free      bool
	freeMatch bool
	adapters  []corelibAdapter
	// fileLiteral is the module source key's Error file literal, interned
	// only when an adapter exists to build a failure from it.
	fileLiteral literalHandle
}

// discoverGeneratedRegex walks one module for std/regex module calls.
func discoverGeneratedRegex(program checker.Program, logicalKey string, literals *literalRegistry) *generatedRegexState {
	state := &generatedRegexState{}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.CorelibCallExpression {
				return nil
			}
			path, _, ok := corelib.FunctionByRuntime(node.Name)
			if !ok {
				return unknownExpressionDiagnostic()
			}
			if path != "std/regex" {
				return nil
			}
			state.used = true
			switch node.Name {
			case "hex_regex_compile":
				state.compile = true
			case "hex_regex_test":
				state.test = true
			case "hex_regex_find":
				state.find = true
			case "hex_regex_capture":
				state.capture = true
			case "hex_regex_free":
				state.free = true
			case "hex_regex_free_match":
				state.freeMatch = true
			default:
				return unknownExpressionDiagnostic()
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return state
	}
	state.adapters = discoverModuleAddonAdapters(program, "std/regex")
	if len(state.adapters) > 0 {
		state.fileLiteral = literals.Intern(logicalKey)
	}
	return state
}

// regexComponents returns the std/regex component pair the reachable
// operations demand. The adapter units are selected exactly when a compile,
// test, find, capture, or free operation is reachable; Span/Match/Pattern
// naming and free_match alone materialize nothing.
func regexComponents(merged *programEmission) ([]componentArtifact, error) {
	state := merged.regexState
	if state == nil || !state.used {
		return nil, nil
	}
	adapter := state.compile || state.test || state.find || state.capture || state.free
	model := regexSourceModel{
		Program:                 state,
		Adapter:                 adapter,
		MaxPatternBytes:         config.RegexMaxPatternBytes,
		MaxCompiledPatternBytes: config.RegexMaxCompiledPatternBytes,
		MaxParenthesisDepth:     config.RegexMaxParenthesisDepth,
		MatchLimit:              config.RegexMatchLimit,
		MatchDepthLimit:         config.RegexMatchDepthLimit,
		MatchHeapLimitKiB:       config.RegexMatchHeapLimitKiB,
	}
	return []componentArtifact{
		{key: "hexal/regex.h", template: "regex.h", model: model},
		{key: "hexal/regex.c", template: "regex.c", model: model},
	}, nil
}

type regexSourceModel struct {
	Program                 *generatedRegexState
	Adapter                 bool
	MaxPatternBytes         int
	MaxCompiledPatternBytes int
	MaxParenthesisDepth     int
	MatchLimit              int
	MatchDepthLimit         int
	MatchHeapLimitKiB       int
}

// AnyCompile and AnyMatch gate the shared compile-side and match-side
// helpers; NeedCompile, NeedTest, NeedFind, NeedCapture, and NeedFree gate
// each raw entry point so one renders only when its operation is reachable.
// The model reaches templates as a value, so these are value-receiver
// exported methods.
func (model regexSourceModel) AnyCompile() bool {
	p := model.Program
	return p.compile || p.test || p.find || p.capture
}

func (model regexSourceModel) AnyMatch() bool {
	return model.Program.test || model.Program.find || model.Program.capture
}

func (model regexSourceModel) NeedCompile() bool { return model.Program.compile }
func (model regexSourceModel) NeedTest() bool    { return model.Program.test }
func (model regexSourceModel) NeedFind() bool    { return model.Program.find }
func (model regexSourceModel) NeedCapture() bool { return model.Program.capture }
func (model regexSourceModel) NeedFree() bool    { return model.Program.free }

// mergeRegexInto unions one module's regex demand into the program state.
func mergeRegexInto(merged, state *generatedRegexState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.compile = merged.compile || state.compile
	merged.test = merged.test || state.test
	merged.find = merged.find || state.find
	merged.capture = merged.capture || state.capture
	merged.free = merged.free || state.free
	merged.freeMatch = merged.freeMatch || state.freeMatch
}

// moduleRegexComponent selects the regex component header one module's
// checker reaches.
func moduleRegexComponent(emission *moduleEmission) []string {
	if emission.regexState == nil || !emission.regexState.used {
		return nil
	}
	return []string{"hexal/regex.h"}
}

// elementNeedsRegex reports whether one collection element's record is owned
// by the core hexal/regex.h artifact: a Span, Match, Pattern, or the
// Span | Nil capture member. A shared component spelling one names that
// header the same way it names hexal/json.h for a JSON record element.
func elementNeedsRegex(element compilerTypes.Type) bool {
	return compilerTypes.IsRegexSpan(element) || compilerTypes.IsRegexMatch(element) ||
		compilerTypes.IsRegexPattern(element) || compilerTypes.IsRegexCaptureMember(element)
}
