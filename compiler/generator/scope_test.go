package generator

import "testing"

func TestPopScopeRejectsDepthZeroAndRoot(t *testing.T) {
	want := "[Unknown Error internal.generator-scope-underflow] generator scope stack has no nested scope to remove"

	empty := newExpressionValidation()
	if err := empty.popScope(); err == nil || err.Error() != want {
		t.Fatalf("empty popScope() = %v, want %q", err, want)
	}
	if len(empty.activeScopes) != 0 {
		t.Fatalf("depth-zero pop changed the stack: %d scopes", len(empty.activeScopes))
	}

	root := newExpressionValidation()
	root.pushScope()
	root.activeScopes[0][1] = true
	if err := root.popScope(); err == nil || err.Error() != want {
		t.Fatalf("root popScope() = %v, want %q", err, want)
	}
	if len(root.activeScopes) != 1 || !root.activeScopes[0][1] {
		t.Fatal("root pop changed the stack")
	}
}

func TestRequireRootScopeChecksOwnerBoundary(t *testing.T) {
	state := newExpressionValidation()
	state.pushScope()
	if err := state.requireRootScope("validated method"); err != nil {
		t.Fatalf("balanced owner rejected: %v", err)
	}

	state.pushScope()
	if err := state.requireRootScope("validated method"); err == nil || err.Error() != "[Unknown Error internal.generator-scope-depth-mismatch] generator scope depth mismatch after validated method" {
		t.Fatalf("leaked child scope = %v", err)
	}
}
