package generator

import (
	"strings"

	"hexal/compiler/checker"
	"hexal/compiler/corelib"
)

// std/json and std/regex module adapters: the module-owned inline adapters
// call the component pair's raw entry points, exactly as std/program and
// std/entropy do. The declarations ship beside the module's other helper
// prototypes; the adapters spell the module-owned result unions with the
// module's own file literal and source site.
//
// std/json: parse -> Value|Error, value.stringify -> String|Error, value.free
// -> no value. std/regex: compile -> Pattern|Error, pattern.test -> Bool|Error,
// pattern.find -> Span|Nil|Error, pattern.capture -> Match|Nil|Error,
// pattern.free and match.free -> no value. Per the dependency-demand table,
// free alone materializes no component beyond the value helpers, and
// match.free alone selects nothing.

// discoverModuleAddonAdapters walks one module's checked calls to the given
// std module and collects the reachable unioned results. A no-value cleanup
// call renders its raw entry point directly at the call site and carries no
// adapter, and a result shape the family does not declare fails closed.
func discoverModuleAddonAdapters(program checker.Program, path string) []corelibAdapter {
	adapters := []corelibAdapter(nil)
	seen := make(map[string]bool)
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.CorelibCallExpression {
				return nil
			}
			nodePath, function, ok := corelib.FunctionByRuntime(node.Name)
			if !ok {
				return unknownExpressionDiagnostic()
			}
			if nodePath != path ||
				function.Result == corelib.ResultNoValue ||
				function.Result == corelib.ResultSize ||
				corelibDirectResult(nodePath, function.Result) {
				return nil
			}
			query := addonRawResultName(path, function.Result)
			if query == "" {
				return unknownExpressionDiagnostic()
			}
			app := routeApplication(node)
			key := addonAdapterSuffix(node.Name, node.ResultType, app) + "|" + node.Name
			if !seen[key] {
				seen[key] = true
				adapters = append(adapters, corelibAdapter{
					runtime: node.Name,
					result:  function.Result,
					params:  function.Params,
					union:   node.ResultType,
					query:   query,
					app:     app,
				})
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	return adapters
}

// addonRawResultName spells the raw result record one std/json or std/regex
// adapter wraps. The corelib mapping cannot carry these shapes: ResultString
// names std/program's record, which differs from std/json's stringify record.
func addonRawResultName(path string, result corelib.Result) string {
	switch path {
	case "std/json":
		switch result {
		case corelib.ResultValue:
			return "hex_json_value_result"
		case corelib.ResultString:
			return "hex_json_string_result"
		}
	case "std/http":
		switch result {
		case corelib.ResultNil:
			return "hex_http_status_result"
		case corelib.ResultServer:
			return "hex_http_server_result"
		case corelib.ResultReadBody:
			return "hex_http_read_result"
		case corelib.ResultBytesNil:
			return "hex_http_bytes_result"
		}
	case "std/regex":
		switch result {
		case corelib.ResultPattern:
			return "hex_regex_pattern_result"
		case corelib.ResultBool:
			return "hex_regex_bool_result"
		case corelib.ResultSpanNil:
			return "hex_regex_span_result"
		case corelib.ResultMatchNil:
			return "hex_regex_match_result"
		}
	}
	return ""
}

// writeJSONInlineHelpers renders the module-owned std/json adapters. A
// free-only program reaches no unioned result and emits no adapter.
func writeJSONInlineHelpers(result *strings.Builder, input *moduleHeaderInput) error {
	if input.jsonState == nil || !input.jsonState.used {
		return nil
	}
	return writeAddonInlineHelpers(result, input, input.jsonState.adapters, input.jsonState.fileLiteral)
}

// writeRegexInlineHelpers renders the module-owned std/regex adapters.
func writeRegexInlineHelpers(result *strings.Builder, input *moduleHeaderInput) error {
	if input.regexState == nil || !input.regexState.used {
		return nil
	}
	return writeAddonInlineHelpers(result, input, input.regexState.adapters, input.regexState.fileLiteral)
}

// writeServerInlineHelpers renders the module-owned std/http adapters.
func writeServerInlineHelpers(result *strings.Builder, input *moduleHeaderInput) error {
	if input.serverState == nil || !input.serverState.used {
		return nil
	}
	return writeAddonInlineHelpers(result, input, input.serverState.adapters, input.serverState.fileLiteral)
}

// writeAddonInlineHelpers is the shared std/json and std/regex header
// writer: both families expose their raw entry points under the _raw suffix
// and both build failures from this module's source key.
func writeAddonInlineHelpers(result *strings.Builder, input *moduleHeaderInput, adapters []corelibAdapter, fileLiteral literalHandle) error {
	if len(adapters) == 0 {
		return nil
	}
	file := "&" + input.stringState.CName(fileLiteral)
	return writeAdapterHelpers(result, adapters, file, input.tags, func(adapter corelibAdapter) string {
		return adapter.runtime + "_raw"
	})
}
