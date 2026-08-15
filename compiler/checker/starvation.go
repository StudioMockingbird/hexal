package checker

import (
	"go/constant"

	compilerTypes "hexal/compiler/types"
)

// RFC 0037 starvation check: when a program links the scheduler runtime, every
// literal `while true` loop in code reachable from the entry point or a direct
// spawn entry must execute an explicit Task.yield() call on every path that
// repeats the loop. Paths ending in break targeting that loop, return, or an
// unrecoverable trap do not repeat and need no yield. Yields inside
// conditionals cover only the paths that execute them; yields inside nested
// loops and calls to helper functions never count.

// checkStarvation applies the must-yield rule to a checked program. It is a
// no-op for programs that do not use any RFC 0037 runtime feature.
func checkStarvation(program Program) compilerTypes.Diagnostics {
	scanner := &starvationScanner{byName: make(map[string][]Statement)}
	scanner.scanRoot(program.Statements)
	for _, declaration := range program.SpecializedFunctions {
		scanner.byName[declaration.Name] = declaration.Body
		scanner.scanStatements(declaration.Body)
	}
	if !scanner.linked {
		return nil
	}
	scanner.diagnoseRoot(program.Statements)
	for _, name := range scanner.spawnEntries {
		if body, ok := scanner.byName[name]; ok {
			scanner.diagnoseFunction(name, body)
		}
	}
	return scanner.diagnostics
}

// starvationScanner collects the named-function call graph and reports
// starving loops.
type starvationScanner struct {
	byName       map[string][]Statement
	spawnEntries []string
	linked       bool
	visited      map[string]bool
	diagnostics  compilerTypes.Diagnostics
}

// scanRoot records scheduler linkage and spawn entries reachable from the
// entry-point statement list.
func (scanner *starvationScanner) scanRoot(statements []Statement) {
	for _, statement := range statements {
		scanner.scanStatement(statement)
		if declaration, ok := statement.(FunctionDeclaration); ok {
			scanner.byName[declaration.Name] = declaration.Body
			scanner.scanStatements(declaration.Body)
		}
	}
}

func (scanner *starvationScanner) scanStatements(statements []Statement) {
	for _, statement := range statements {
		scanner.scanStatement(statement)
	}
}

// scanStatement walks one statement for RFC 0037 expression kinds. Spawn
// targets become independent roots; any other concurrency kind links the
// scheduler runtime.
func (scanner *starvationScanner) scanStatement(statement Statement) {
	switch statement := statement.(type) {
	case CallStatement:
		scanner.scanOperand(statement.Call)
	case Declaration:
		scanner.scanOperand(statement.Source)
	case Assignment:
		scanner.scanOperand(statement.Source)
	case IfStatement:
		scanner.scanOperand(statement.Condition)
		scanner.scanStatements(statement.Then)
		for _, branch := range statement.ElseIf {
			scanner.scanOperand(branch.Condition)
			scanner.scanStatements(branch.Body)
		}
		scanner.scanStatements(statement.Else)
	case WhileStatement:
		scanner.scanOperand(statement.Condition)
		scanner.scanStatements(statement.Body)
	case ForStatement:
		scanner.scanOperand(statement.Source)
		scanner.scanStatements(statement.Body)
	case ReturnStatement:
		if statement.Value != nil {
			scanner.scanOperand(*statement.Value)
		}
	case DeferStatement:
		scanner.scanOperand(statement.Expression)
	case ErrdeferStatement:
		scanner.scanOperand(statement.Expression)
	case FunctionDeclaration:
		scanner.byName[statement.Name] = statement.Body
		scanner.scanStatements(statement.Body)
	}
}

// scanOperand records linkage and spawn entries in one checked expression.
func (scanner *starvationScanner) scanOperand(operand Operand) {
	if operand.Node.Kind == SpawnExpression {
		scanner.linked = true
		if name := spawnTargetName(operand.Node); name != "" {
			scanner.spawnEntries = append(scanner.spawnEntries, name)
		}
	}
	scanOperandTree(operand, func(node Expression) {
		switch node.Kind {
		case SpawnExpression:
			scanner.linked = true
			if name := spawnTargetName(node); name != "" {
				scanner.spawnEntries = append(scanner.spawnEntries, name)
			}
		case TaskYieldExpression, TaskMethodCallExpression,
			ChannelConstructorExpression, ChannelMethodCallExpression,
			MutexConstructorExpression, MutexMethodCallExpression,
			AtomicConstructorExpression, AtomicMethodCallExpression:
			scanner.linked = true
		}
	})
}

// spawnTargetName extracts the named callee of a checked spawn node.
func spawnTargetName(node Expression) string {
	if node.Operand == nil || node.Operand.Kind != CallExpression || node.Operand.Operand == nil {
		return ""
	}
	if node.Operand.Operand.Kind != FunctionReferenceExpression {
		return ""
	}
	return node.Operand.Operand.Name
}

// diagnoseRoot applies the starvation rule to the entry-point statements.
func (scanner *starvationScanner) diagnoseRoot(statements []Statement) {
	scanner.diagnoseStatements(statements)
}

// diagnoseFunction follows direct named-function calls from one root and
// checks every reachable literal while-true loop.
func (scanner *starvationScanner) diagnoseFunction(name string, body []Statement) {
	if scanner.visited == nil {
		scanner.visited = make(map[string]bool)
	}
	if scanner.visited[name] {
		return
	}
	scanner.visited[name] = true
	scanner.diagnoseStatements(body)
}

// diagnoseStatements walks one statement list, checking literal while-true
// loops and descending into direct named-function calls.
func (scanner *starvationScanner) diagnoseStatements(statements []Statement) {
	for _, statement := range statements {
		switch statement := statement.(type) {
		case WhileStatement:
			if isLiteralTrue(statement.Condition, statement.ConditionKnown) && loopMayRepeatWithoutYield(statement.Body) {
				scanner.diagnostics = append(scanner.diagnostics, compilerTypes.Diagnostic{
					Category: compilerTypes.SemanticError,
					Stage:    "checker",
					Line:     statement.SourceLine,
					Column:   statement.SourceColumn,
					Message:  "while true loop must execute Task.yield() on every repeating path",
				})
			}
			scanner.diagnoseStatements(statement.Body)
		case IfStatement:
			scanner.diagnoseStatements(statement.Then)
			for _, branch := range statement.ElseIf {
				scanner.diagnoseStatements(branch.Body)
			}
			scanner.diagnoseStatements(statement.Else)
		case ForStatement:
			scanner.diagnoseStatements(statement.Body)
		case CallStatement:
			if name := directCallName(statement.Call); name != "" {
				if body, ok := scanner.byName[name]; ok {
					scanner.diagnoseFunction(name, body)
				}
			}
		}
	}
}

// directCallName reports the named callee of a direct function-call statement.
func directCallName(call Operand) string {
	if call.Node.Kind != CallExpression || call.Node.Operand == nil || call.Node.Operand.Kind != FunctionReferenceExpression {
		return ""
	}
	return call.Node.Operand.Name
}

// isLiteralTrue reports whether a checked while condition is the constant
// `true`: either the literal spelling or the known-value metadata of a named
// immutable Bool binding (the read itself stays in the condition, so the
// metadata must be consulted separately). Any other spelling, even one that
// always evaluates true, is not the narrowed literal form the rule rejects.
func isLiteralTrue(condition Operand, conditionKnown *Operand) bool {
	if condition.Kind == ConstantOperand && condition.Constant != nil && condition.Constant.Kind() == constant.Bool {
		return constant.BoolVal(condition.Constant)
	}
	if conditionKnown != nil && conditionKnown.Kind == ConstantOperand && conditionKnown.Constant != nil && conditionKnown.Constant.Kind() == constant.Bool {
		return constant.BoolVal(conditionKnown.Constant)
	}
	return false
}

// loopMayRepeatWithoutYield reports whether some execution path through the
// body reaches the loop backedge without executing a direct Task.yield()
// call. A path ending in break targeting the loop, return, or a nested loop
// that can never fall through does not repeat the checked loop.
func loopMayRepeatWithoutYield(body []Statement) bool {
	bad := true // a path has arrived without yielding
	for _, statement := range body {
		switch statement := statement.(type) {
		case CallStatement:
			if statement.Call.Node.Kind == TaskYieldExpression {
				bad = false
			}
		case IfStatement:
			afterThen := branchMayRepeatWithoutYield(statement.Then, bad)
			bad = afterThen
			for _, branch := range statement.ElseIf {
				bad = branchMayRepeatWithoutYield(branch.Body, bad)
			}
			if len(statement.Else) > 0 {
				bad = branchMayRepeatWithoutYield(statement.Else, bad)
			}
			// No else: the skip path arrives with the pre-if state, which
			// branchMayRepeatWithoutYield already folded into bad above.
		case WhileStatement:
			if !loopFallsThrough(statement) {
				bad = false // nested loop never returns; the path stops here
			}
		case ForStatement:
			// A for loop may iterate zero times and always falls through.
		case BreakStatement:
			bad = false // leaves the checked loop; does not repeat it
		case ContinueStatement:
			if bad {
				return true // repeats the checked loop without yielding
			}
		case ReturnStatement:
			bad = false // leaves the function; does not repeat the loop
		}
	}
	return bad
}

// branchMayRepeatWithoutYield computes the arrival state after one if/else
// branch body given the arrival state before it.
func branchMayRepeatWithoutYield(body []Statement, incoming bool) bool {
	bad := incoming
	for _, statement := range body {
		switch statement := statement.(type) {
		case CallStatement:
			if statement.Call.Node.Kind == TaskYieldExpression {
				bad = false
			}
		case IfStatement:
			afterThen := branchMayRepeatWithoutYield(statement.Then, bad)
			bad = afterThen
			for _, branch := range statement.ElseIf {
				bad = branchMayRepeatWithoutYield(branch.Body, bad)
			}
			if len(statement.Else) > 0 {
				bad = branchMayRepeatWithoutYield(statement.Else, bad)
			}
		case WhileStatement:
			if !loopFallsThrough(statement) {
				bad = false
			}
		case ForStatement:
		case BreakStatement:
			// Break targets the loop enclosing this branch, so the path
			// stops at the loop; it does not reach the branch's end.
			return false
		case ContinueStatement:
			// Continue targets the loop enclosing this branch, not the
			// checked loop; it is an ordinary repeating path of that inner
			// loop and reaches this branch's end through the loop backedge.
			if !bad {
				return false
			}
		case ReturnStatement:
			return false
		}
	}
	return bad
}

// loopFallsThrough reports whether a nested loop can reach its end: a
// condition-controlled loop or for loop always can, while a literal while-true
// loop can only fall through when its body contains a break targeting it.
func loopFallsThrough(statement WhileStatement) bool {
	if !isLiteralTrue(statement.Condition, statement.ConditionKnown) {
		return true
	}
	return loopContainsDirectBreak(statement.Body)
}

// loopContainsDirectBreak reports whether a break targeting the enclosing
// loop appears directly in the body, without crossing a nested loop.
func loopContainsDirectBreak(body []Statement) bool {
	for _, statement := range body {
		switch statement := statement.(type) {
		case BreakStatement:
			return true
		case WhileStatement:
			// A nested loop's breaks target the nested loop, not this one.
		case IfStatement:
			if loopContainsDirectBreak(statement.Then) {
				return true
			}
			for _, branch := range statement.ElseIf {
				if loopContainsDirectBreak(branch.Body) {
					return true
				}
			}
			if loopContainsDirectBreak(statement.Else) {
				return true
			}
		}
	}
	return false
}

// scanOperandTree visits every checked expression node in one operand tree,
// including nested call arguments and control-flow subexpressions.
func scanOperandTree(operand Operand, visit func(Expression)) {
	visitNode := func(node Expression) {
		visit(node)
		if node.Operand != nil {
			scanOperandTree(Operand{Node: *node.Operand}, visit)
		}
		if node.Left != nil {
			scanOperandTree(Operand{Node: *node.Left}, visit)
		}
		if node.Right != nil {
			scanOperandTree(Operand{Node: *node.Right}, visit)
		}
		for _, argument := range node.Arguments {
			scanOperandTree(argument, visit)
		}
	}
	visitNode(operand.Node)
}
