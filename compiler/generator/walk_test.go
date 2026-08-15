package generator

import (
	"fmt"
	"strings"
	"testing"

	"hexal/compiler/checker"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// The shared walker must visit every statement and expression shape the
// discovery passes relied on, in deterministic pre-order. The
// unknown-statement branch of the walker is unreachable from outside the
// checker package (checker.Statement's marker method is unexported), so the
// fail-closed behavior is enforced by the branch itself, not a test.

func walkTestProgram(t *testing.T, source string) (string, error) {
	t.Helper()
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	checked, err := checker.Check(program)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	var visited []string
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			visited = append(visited, "T:"+typ.Name)
			return nil
		},
		Operand: func(operand checker.Operand) error {
			visited = append(visited, "O:"+operand.Type.Name)
			return nil
		},
		Expression: func(node checker.Expression) error {
			visited = append(visited, fmt.Sprintf("E:%d", node.Kind))
			return nil
		},
	}
	err = walkProgram(checked, visitor)
	return strings.Join(visited, " "), err
}

func TestWalkProgramCoversEveryStatementShape(t *testing.T) {
	visited, err := walkTestProgram(t, `
type Pair = { mut values: Array<Int32, 2>, }
type Box<T> = { value: T }
impl Box<T>.get(): T do
    return self.value
end
fun area(radius: Float64): Float64 do
    return 3.14 * radius
end
fun onerror(): Int32 | Error do
    errdefer print("err")
    return 0
end
fun read_count(): Int32 | Error do
    return 0
end
fun demo(h: Heap): Int32 | Error do
    mut count: Int32 = 1
    count = count + 1
    print("hi")
    mut value: Int32 = 3
    value = value.to<UInt8>().to<Int32>()
    defer print("bye")
    values: Array<Int32, 3> = [1, 2, 3]
    for i in values do
        if count > 0 then
            count = count - 1
            continue
        else
            break
        end
    end
    while count < 2 do
        count = count + 1
    end
    box: Box<Int32> = Box<Int32> { value = 1 }
    box.get()
    viaTry: Int32 = try read_count()
    return count
end
`)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, expected := range []int{int(checker.ConstantExpression), int(checker.CallExpression),
		int(checker.MethodCallExpression), int(checker.ConversionExpression), int(checker.TryExpression)} {
		if !strings.Contains(visited, fmt.Sprintf("E:%d", expected)) {
			t.Errorf("walk did not visit kind %d; got %s", expected, visited)
		}
	}
}

func TestWalkProgramIsDeterministicPreOrder(t *testing.T) {
	source := "fun demo(count: Int32): Int32 do\n    total: Int32 = count + 2\n    return total\nend\n"
	first, err := walkTestProgram(t, source)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for range 3 {
		again, err := walkTestProgram(t, source)
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if again != first {
			t.Fatalf("walk is not deterministic:\nfirst: %s\nagain: %s", first, again)
		}
	}
	// Pre-order: the declaration's types are visited before its source's
	// children.
	declaration := strings.Index(first, "T:Int32")
	binary := strings.Index(first, fmt.Sprintf("E:%d", checker.BinaryOperationExpression))
	if declaration < 0 || binary < 0 || declaration > binary {
		t.Fatalf("expected declaration types before expression children: %s", first)
	}
}

func TestWalkProgramAcceptsNilVisitor(t *testing.T) {
	source := "fun demo(count: Int32): Int32 do\n    total: Int32 = count + 2\n    return total\nend\n"
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	checked, err := checker.Check(program)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := walkProgram(checked, nil); err != nil {
		t.Fatalf("nil visitor must be a no-op walk, got %v", err)
	}
}
