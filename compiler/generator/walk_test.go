package generator

import (
	"fmt"
	"strings"
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The shared walker must visit every statement and expression shape the
// discovery passes relied on, in deterministic pre-order. The
// unknown-statement branch of the walker is unreachable from outside the
// checker package (checker.Statement's marker method is unexported), so the
// fail-closed behavior is enforced by the branch itself, not a test.

// The hoist traversal walks statement values: taking addresses of fields on
// type-switch copies would heap-escape a whole operand per visited node on
// every hoist pass. Zero allocations here pins the value-traversal contract.
func TestWalkStatementExpressionsAllocatesNothing(t *testing.T) {
	program := checkedGeneratorSource(t, "mut total: Int32 := 0\nfun add(value: Int32): Int32 do\n    return value + 1\nend\n")
	statement := program.Statements[0]
	visit := func(checker.Expression) error { return nil }
	if allocations := testing.AllocsPerRun(100, func() {
		_ = walkStatementExpressions(statement, visit)
	}); allocations != 0 {
		t.Fatalf("walkStatementExpressions AllocsPerRun = %v, want 0", allocations)
	}
}

func walkTestProgram(t *testing.T, source string) string {
	t.Helper()
	checked := checkedGeneratorSource(t, source)
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
	if err := walkProgram(checked, visitor); err != nil {
		t.Fatalf("walk: %v", err)
	}
	return strings.Join(visited, " ")
}

func TestWalkProgramCoversEveryStatementShape(t *testing.T) {
	visited := walkTestProgram(t, `
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
    mut count: Int32 := 1
    count = count + 1
    print("hi")
    mut value: Int32 := 3
    value = value.to<UInt8>().to<Int32>()
    defer print("bye")
    values: Array<Int32, 3> := [1, 2, 3]
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
    box: Box<Int32> := Box<Int32> { value = 1 }
    box.get()
    viaTry: Int32 := try read_count()
    return count
end
`)
	for _, expected := range []int{int(checker.ConstantExpression), int(checker.CallExpression),
		int(checker.MethodCallExpression), int(checker.ConversionExpression), int(checker.TryExpression)} {
		if !strings.Contains(visited, fmt.Sprintf("E:%d", expected)) {
			t.Errorf("walk did not visit kind %d; got %s", expected, visited)
		}
	}
}

func TestWalkProgramIsDeterministicPreOrder(t *testing.T) {
	source := "fun demo(count: Int32): Int32 do\n    total: Int32 := count + 2\n    return total\nend\n"
	first := walkTestProgram(t, source)
	for range 3 {
		again := walkTestProgram(t, source)
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
	checked := checkedGeneratorSource(t, "fun demo(count: Int32): Int32 do\n    total: Int32 := count + 2\n    return total\nend\n")
	if err := walkProgram(checked, nil); err != nil {
		t.Fatalf("walk: %v", err)
	}
}
