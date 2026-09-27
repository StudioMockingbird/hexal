package checker

import "testing"

func TestInlineListTypeChecksBeforeGeneration(t *testing.T) {
	source := "fun demo() do\n    let mut values: List<Int32, 4> = []\n    values.push(7)\n    values[0] = 9\n    let copy: List<Int32, 4> = values\n    let size: Size = copy.length()\n    let same: Bool = values == copy\n    values.clear()\nend"
	if _, err := Check(parseProgram(t, source)); err != nil {
		t.Fatalf("Check rejected inline List: %v", err)
	}
}
