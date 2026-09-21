package checker

// Environment analysis: before any function body is checked, compute each
// entry-module named function or method's direct captures and direct callees
// from the parser tree, then take the least fixed point of environment
// dependence over the call graph. The result drives the non-escaping and
// initialization-safety diagnostics, which must be known while bodies and root
// statements are checked.

import (
	"sort"

	"hexal/compiler/parser"
)

// captureScope is one lexical shadowing frame during capture analysis.
type captureScope struct {
	names  map[string]bool
	parent *captureScope
}

func (scope *captureScope) has(name string) bool {
	for current := scope; current != nil; current = current.parent {
		if current.names[name] {
			return true
		}
	}
	return false
}

// bodyAnalysis is one function or method's direct captures and direct callees.
type bodyAnalysis struct {
	captures map[string]bool
	callees  map[string]bool
}

// analyzeBody computes the direct captures and direct callees of one function
// or method body. visible is the set of root binding names declared before the
// declaration; params and self seed the initial shadowing scope.
func analyzeBody(body []parser.Statement, params []string, self bool, visible map[string]bool) bodyAnalysis {
	analysis := bodyAnalysis{captures: make(map[string]bool), callees: make(map[string]bool)}
	scope := &captureScope{names: make(map[string]bool)}
	for _, param := range params {
		scope.names[param] = true
	}
	if self {
		scope.names["self"] = true
	}
	analysis.statements(body, scope, visible)
	return analysis
}

func (analysis *bodyAnalysis) statements(statements []parser.Statement, scope *captureScope, visible map[string]bool) {
	for _, statement := range statements {
		switch node := statement.(type) {
		case parser.Declaration:
			analysis.expression(node.Initializer, scope, visible)
			scope.names[node.Name.Lexeme] = true
		case parser.Assignment:
			analysis.expression(node.Target, scope, visible)
			analysis.expression(node.Initializer, scope, visible)
		case parser.CallExpression:
			analysis.expression(node, scope, visible)
		case parser.TryStatement:
			analysis.expression(node.Operand, scope, visible)
		case parser.ReturnStatement:
			if node.Value != nil {
				analysis.expression(node.Value, scope, visible)
			}
		case parser.IfStatement:
			analysis.expression(node.Condition, scope, visible)
			analysis.block(node.Then, scope, visible)
			for _, branch := range node.ElseIf {
				analysis.expression(branch.Condition, scope, visible)
				analysis.block(branch.Body, scope, visible)
			}
			analysis.block(node.Else, scope, visible)
		case parser.WhileStatement:
			analysis.expression(node.Condition, scope, visible)
			analysis.block(node.Body, scope, visible)
		case parser.ForStatement:
			analysis.expression(node.Source, scope, visible)
			body := &captureScope{names: make(map[string]bool), parent: scope}
			for _, binder := range node.Binders {
				body.names[binder.Name.Lexeme] = true
			}
			analysis.statements(node.Body, body, visible)
		case parser.UnsafeStatement:
			analysis.block(node.Body, scope, visible)
		case parser.DeferStatement:
			analysis.expression(node.Expression, scope, visible)
		case parser.ErrdeferStatement:
			analysis.expression(node.Expression, scope, visible)
		}
	}
}

func (analysis *bodyAnalysis) block(body []parser.Statement, scope *captureScope, visible map[string]bool) {
	child := &captureScope{names: make(map[string]bool), parent: scope}
	analysis.statements(body, child, visible)
}

func (analysis *bodyAnalysis) expression(expression parser.Expression, scope *captureScope, visible map[string]bool) {
	switch node := expression.(type) {
	case parser.VariableExpression:
		name := node.Name.Lexeme
		if !scope.has(name) && visible[name] {
			analysis.captures[name] = true
		}
	case parser.CallExpression:
		switch callee := node.Callee.(type) {
		case parser.VariableExpression:
			if !scope.has(callee.Name.Lexeme) {
				analysis.callees[callee.Name.Lexeme] = true
			}
		case parser.PropertyExpression:
			analysis.expression(callee.Receiver, scope, visible)
			analysis.callees[callee.Property.Lexeme] = true
		default:
			analysis.expression(node.Callee, scope, visible)
		}
		for _, argument := range node.Arguments {
			analysis.expression(argument, scope, visible)
		}
	case parser.BinaryExpression:
		analysis.expression(node.Left, scope, visible)
		analysis.expression(node.Right, scope, visible)
	case parser.UnaryExpression:
		analysis.expression(node.Operand, scope, visible)
	case parser.PropertyExpression:
		analysis.expression(node.Receiver, scope, visible)
	case parser.IndexExpression:
		analysis.expression(node.Receiver, scope, visible)
		analysis.expression(node.Index, scope, visible)
	case parser.ArrayLiteralExpression:
		for _, element := range node.Elements {
			analysis.expression(element, scope, visible)
		}
	case parser.TypeTestExpression:
		analysis.expression(node.Operand, scope, visible)
	case parser.MatchExpression:
		analysis.expression(node.Scrutinee, scope, visible)
		for _, arm := range node.Arms {
			analysis.expression(arm.Expression, scope, visible)
		}
	case parser.AddressExpression:
		analysis.expression(node.Place, scope, visible)
	case parser.DereferenceExpression:
		analysis.expression(node.Operand, scope, visible)
	case parser.NegatedNumericLiteral:
		analysis.expression(node.Literal, scope, visible)
	case parser.TryExpression:
		analysis.expression(node.Operand, scope, visible)
	case parser.SpawnExpression:
		analysis.expression(node.Operand, scope, visible)
	case parser.AnonymousFunctionLiteral:
		// A local or anonymous literal never captures, so its body
		// contributes no capture to the enclosing declaration.
	}
}

// entryEnvironmentAnalysis is the pre-body environment analysis for one entry
// module: each declaration's direct captures and callees, and the resulting
// environment dependence by declaration name.
type entryEnvironmentAnalysis struct {
	captures     map[string]map[string]bool
	envDependent map[string]bool
}

// analyzeEntryEnvironment walks one entry module's top level in source order,
// computing each named function or method's direct captures and callees against
// the root bindings declared before it, then closes environment dependence over
// the call graph.
func analyzeEntryEnvironment(program parser.Program) entryEnvironmentAnalysis {
	analysis := entryEnvironmentAnalysis{
		captures:     make(map[string]map[string]bool),
		envDependent: make(map[string]bool),
	}
	callees := make(map[string]map[string]bool)
	visible := make(map[string]bool)
	for _, item := range program.Items {
		switch declaration := item.(type) {
		case parser.Declaration:
			if _, isSugar := directFunctionLiteralSugar(declaration); isSugar {
				body := analyzeBody(declaration.Initializer.(parser.AnonymousFunctionLiteral).Body, parameterNames(nil), false, visible)
				analysis.record(declaration.Name.Lexeme, body, callees)
				continue
			}
			visible[declaration.Name.Lexeme] = true
		case parser.FunctionDeclaration:
			body := analyzeBody(declaration.Body, parameterNames(declaration.Parameters), false, visible)
			analysis.record(declaration.Name.Lexeme, body, callees)
		case parser.MethodDeclaration:
			body := analyzeBody(declaration.Body, parameterNames(declaration.Parameters), true, visible)
			analysis.record(declaration.Name.Lexeme, body, callees)
		}
	}
	for changed := true; changed; {
		changed = false
		for name, direct := range callees {
			if analysis.envDependent[name] {
				continue
			}
			for callee := range direct {
				if analysis.envDependent[callee] {
					analysis.envDependent[name] = true
					changed = true
					break
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for name, direct := range callees {
			captures := analysis.captures[name]
			if captures == nil {
				captures = make(map[string]bool)
				analysis.captures[name] = captures
			}
			for callee := range direct {
				for capture := range analysis.captures[callee] {
					if !captures[capture] {
						captures[capture] = true
						changed = true
					}
				}
			}
		}
	}
	return analysis
}

func (analysis entryEnvironmentAnalysis) record(name string, body bodyAnalysis, callees map[string]map[string]bool) {
	analysis.captures[name] = body.captures
	callees[name] = body.callees
	if len(body.captures) > 0 {
		analysis.envDependent[name] = true
	}
}

func parameterNames(parameters []parser.Parameter) []string {
	names := make([]string, len(parameters))
	for index, parameter := range parameters {
		names[index] = parameter.Name.Lexeme
	}
	return names
}

// directCallees returns the source names a function or method body directly
// calls: a bare call names a function, a dotted call names a method.
func directCallees(body []parser.Statement) map[string]bool {
	callees := make(map[string]bool)
	var visitExpression func(expression parser.Expression)
	var visitStatements func(statements []parser.Statement)

	visitStatements = func(statements []parser.Statement) {
		for _, statement := range statements {
			switch node := statement.(type) {
			case parser.Declaration:
				visitExpression(node.Initializer)
			case parser.Assignment:
				visitExpression(node.Target)
				visitExpression(node.Initializer)
			case parser.CallExpression:
				visitExpression(node)
			case parser.TryStatement:
				visitExpression(node.Operand)
			case parser.ReturnStatement:
				if node.Value != nil {
					visitExpression(node.Value)
				}
			case parser.IfStatement:
				visitExpression(node.Condition)
				visitStatements(node.Then)
				for _, branch := range node.ElseIf {
					visitExpression(branch.Condition)
					visitStatements(branch.Body)
				}
				visitStatements(node.Else)
			case parser.WhileStatement:
				visitExpression(node.Condition)
				visitStatements(node.Body)
			case parser.ForStatement:
				visitExpression(node.Source)
				visitStatements(node.Body)
			case parser.UnsafeStatement:
				visitStatements(node.Body)
			case parser.DeferStatement:
				visitExpression(node.Expression)
			case parser.ErrdeferStatement:
				visitExpression(node.Expression)
			}
		}
	}

	visitExpression = func(expression parser.Expression) {
		switch node := expression.(type) {
		case parser.CallExpression:
			switch callee := node.Callee.(type) {
			case parser.VariableExpression:
				callees[callee.Name.Lexeme] = true
			case parser.PropertyExpression:
				callees[callee.Property.Lexeme] = true
			}
			for _, argument := range node.Arguments {
				visitExpression(argument)
			}
		case parser.BinaryExpression:
			visitExpression(node.Left)
			visitExpression(node.Right)
		case parser.UnaryExpression:
			visitExpression(node.Operand)
		case parser.PropertyExpression:
			visitExpression(node.Receiver)
		case parser.IndexExpression:
			visitExpression(node.Receiver)
			visitExpression(node.Index)
		case parser.ArrayLiteralExpression:
			for _, element := range node.Elements {
				visitExpression(element)
			}
		case parser.TypeTestExpression:
			visitExpression(node.Operand)
		case parser.MatchExpression:
			visitExpression(node.Scrutinee)
			for _, arm := range node.Arms {
				visitExpression(arm.Expression)
			}
		case parser.AddressExpression:
			visitExpression(node.Place)
		case parser.DereferenceExpression:
			visitExpression(node.Operand)
		case parser.NegatedNumericLiteral:
			visitExpression(node.Literal)
		case parser.TryExpression:
			visitExpression(node.Operand)
		case parser.SpawnExpression:
			visitExpression(node.Operand)
		case parser.AnonymousFunctionLiteral:
			visitStatements(node.Body)
		}
	}

	visitStatements(body)
	return callees
}

// computeEnvironmentDependence sets EnvDependent on every checked entry-module
// function and method by least fixed point over the direct call graph.
func computeEnvironmentDependence(checked *Program) {
	callees := make(map[string]map[string]bool)
	state := make(map[string]bool)
	captures := make(map[string][]Capture)
	for _, statement := range checked.Statements {
		switch declaration := statement.(type) {
		case FunctionDeclaration:
			state[declaration.Name] = declaration.EnvDependent
			callees[declaration.Name] = declaration.DirectCallees
			captures[declaration.Name] = declaration.Captures
		case MethodDeclaration:
			state[declaration.Name] = declaration.EnvDependent
			callees[declaration.Name] = declaration.DirectCallees
			captures[declaration.Name] = declaration.Captures
		}
	}
	for _, declaration := range checked.SpecializedFunctions {
		state[declaration.Name] = declaration.EnvDependent
		callees[declaration.Name] = declaration.DirectCallees
		captures[declaration.Name] = declaration.Captures
	}
	for _, declaration := range checked.SpecializedMethods {
		state[declaration.Name] = declaration.EnvDependent
		callees[declaration.Name] = declaration.DirectCallees
		captures[declaration.Name] = declaration.Captures
	}
	for changed := true; changed; {
		changed = false
		for name, direct := range callees {
			if state[name] {
				continue
			}
			for callee := range direct {
				if state[callee] {
					state[name] = true
					changed = true
					break
				}
			}
		}
	}
	for index, statement := range checked.Statements {
		switch declaration := statement.(type) {
		case FunctionDeclaration:
			declaration.EnvDependent = state[declaration.Name]
			checked.Statements[index] = declaration
		case MethodDeclaration:
			declaration.EnvDependent = state[declaration.Name]
			checked.Statements[index] = declaration
		}
	}
	for changed := true; changed; {
		changed = false
		for name, direct := range callees {
			for callee := range direct {
				for _, capture := range captures[callee] {
					if !containsCapture(captures[name], capture.Name) {
						captures[name] = append(captures[name], capture)
						changed = true
					}
				}
			}
		}
	}
	for index, declaration := range checked.SpecializedFunctions {
		declaration.EnvDependent = state[declaration.Name]
		declaration.Captures = captures[declaration.Name]
		checked.SpecializedFunctions[index] = declaration
	}
	for index, declaration := range checked.SpecializedMethods {
		declaration.EnvDependent = state[declaration.Name]
		declaration.Captures = captures[declaration.Name]
		checked.SpecializedMethods[index] = declaration
	}
}

func containsCapture(captures []Capture, name string) bool {
	for _, capture := range captures {
		if capture.Name == name {
			return true
		}
	}
	return false
}

func inheritEnvironment(envDependent *bool, captures *[]Capture, callees map[string]bool, names *scope) {
	for callee := range callees {
		if names.envDependent[callee] {
			*envDependent = true
		}
		for captureName := range names.envCaptures[callee] {
			if containsCapture(*captures, captureName) {
				continue
			}
			bound, status := names.lookup(captureName)
			if status != nameFound {
				continue
			}
			*captures = append(*captures, Capture{Name: captureName, Binding: bound.id, Type: bound.typ, Mutable: bound.mutable})
		}
	}
	sort.SliceStable(*captures, func(i, j int) bool {
		return (*captures)[i].Binding < (*captures)[j].Binding
	})
}
