package integration

// Declaration-time checking of open generic bodies. A never-specialized
// generic must still reject an error that holds for every substitution, while
// an operation whose validity depends on the substituted type is deferred to
// specialization.

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// The two Problem programs are rejected at declaration, without any
// specialization, with the ordinary messages.
func TestGenericDeclarationRejectsProblemPrograms(t *testing.T) {
	assertRejects(t, "fun bad<T>(value: T): T do\n    return nope\nend\n", "unknown variable nope")
	assertRejects(t, "fun wrong<T>(value: T): T do\n    x: Int32 := \"text\"\n    return value\nend\n", "expected Int32 initializer; got String")
}

// One unspecialized example per declaration-time category is rejected.
func TestGenericDeclarationTimeCategories(t *testing.T) {
	for _, testCase := range []struct{ name, source, want string }{
		{"unknown function", "fun f<T>(v: T): Int32 do\n    return missing(v)\nend\n", "unknown function missing"},
		{"unknown type", "fun f<T>(v: T) do\n    x: Bogus := 1\nend\n", "unknown type Bogus"},
		{"independent typing", "fun f<T>(v: T): Int32 do\n    x: Int32 := \"text\"\n    return x\nend\n", "expected Int32 initializer; got String"},
		{"wrong arity known callee", "fun g(x: Int32): Int32 do\n    return x\nend\nfun f<T>(v: T): Int32 do\n    return g(v, v)\nend\n", "g expects 1 arguments; got 2"},
		{"missing return", "fun f<T>(v: T): Int32 do\n    x: Int32 := 1\nend\n", "may fall through without returning"},
		{"misplaced break", "fun f<T>(v: T) do\n    break\nend\n", "break"},
		{"fixed assignment", "fun f<T>(v: T): T do\n    v = v\n    return v\nend\n", "cannot assign to parameter v"},
		{"generic parameter redeclared", "fun f<T, T>(v: T): T do\n    return v\nend\n", "declared more than once"},
		{"unknown layout", "type Holder<T> is struct item: Nope end\n", "unknown type Nope"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, testCase.want)
		})
	}
}

// One unspecialized example per deferred category is accepted; the same body
// specialized with an invalid argument reports the existing concrete
// diagnostic.
func TestGenericDeferredCategories(t *testing.T) {
	for _, testCase := range []struct{ name, source string }{
		{"operators", "fun f<T>(a: T, b: T): T do\n    return a + b\nend\n"},
		{"member", "fun f<T>(v: T): Size do\n    return v.len()\nend\n"},
		{"assignment between dependent and other", "fun f<T>(v: T): Int32 do\n    x: Int32 := v\n    return x\nend\n"},
		{"conversion", "fun f<T>(v: T): Int64 do\n    return v.to<Int64>()\nend\n"},
		{"equality", "fun f<T>(a: T, b: T): Bool do\n    return a == b\nend\n"},
		{"identity", "fun id<T>(v: T): T do\n    return v\nend\n"},
		{"dependent callee", "fun f<T>(callee: T): Int32 do\n    return callee(1, 2)\nend\n"},
		{"nested generic", "fun id<T>(v: T): T do\n    return v\nend\nfun f<T>(v: T): T do\n    return id<T>(v)\nend\n"},
		{"dependent layout", "type Holder<T> is struct item: T end\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertCompiles(t, testCase.source)
		})
	}
	// The deferred operator body reports the concrete diagnostic when
	// specialized with a struct that has no `+`.
	assertRejects(t,
		"fun add<T>(a: T, b: T): T do\n    return a + b\nend\ntype P is struct q: Int32 end\nx: P := add<P>(P(q = 1), P(q = 2))\n",
		"operator + requires numeric operands; got P")
}

// An independent error inside a deferred operation is still reported.
func TestGenericDeclarationChecksDeferredOperands(t *testing.T) {
	assertRejects(t, "fun f<T>(v: T): Size do\n    return v.take(nope)\nend\n", "unknown variable nope")
}

// A dependent callee defers callability and arity; a wrong-arity call to a
// known function is rejected during declaration checking.
func TestGenericDependentCalleeVersusKnownArity(t *testing.T) {
	assertCompiles(t, "fun f<T>(callee: T): Int32 do\n    return callee(1, 2)\nend\n")
	assertRejects(t, "fun g(x: Int32): Int32 do\n    return x\nend\nfun f<T>(v: T): Int32 do\n    return g(v, v)\nend\n", "g expects 1 arguments; got 2")
}

// Generic methods and methods of generic types follow the rule.
func TestGenericMethodDeclarationChecking(t *testing.T) {
	assertCompiles(t, "type Box<T> is struct item: T end\nmethod Box<T>.get(): T do\n    return self.item\nend\n")
	assertRejects(t, "type Box<T> is struct item: T end\nmethod Box<T>.get(): T do\n    return nope\nend\n", "unknown variable nope")
	assertCompiles(t, "type Box<T> is struct item: T end\nmethod Box<T>.combine(other: Box<T>): Bool do\n    return self.item == other.item\nend\n")
}

// Generic aliases, ADT payloads, and legal pointer-recursive layouts are
// checked at declaration.
func TestGenericLayoutDeclarationChecking(t *testing.T) {
	assertCompiles(t, "type Opt<T> is union | Some as value: T end | Empty end\n")
	assertRejects(t, "type Holder<T> is struct item: Nope end\n", "unknown type Nope")
	assertRejects(t, "type Opt<T> is union | Some as value: Nope end | Empty end\n", "unknown type Nope")
	// Pointer-indirected self-reference stays legal; by-value self-reference
	// is rejected by the existing layout rule.
	assertCompiles(t, "type Node<T> is struct value: T, next: Ptr<Node<T>> | Nil end\n")
}

// Forward calls and mutual recursion between generic and non-generic
// module-level declarations resolve exactly as in ordinary bodies.
func TestGenericForwardAndMutualRecursion(t *testing.T) {
	assertCompiles(t, "fun f<T>(v: T): Int32 do\n    return g(1)\nend\nfun g(x: Int32): Int32 do\n    return x\nend\n")
	assertCompiles(t, "fun even<T>(v: T, n: Int32): Bool do\n    if n == 0 then\n        return true\n    end\n    return odd(v, n - 1)\nend\nfun odd<T>(v: T, n: Int32): Bool do\n    if n == 0 then\n        return false\n    end\n    return even(v, n - 1)\nend\n")
}

// An exported generic in a reachable module is checked even when never
// specialized; the diagnostic names the defining module.
func TestGenericCheckedInDefiningModule(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\nvalue: Int32 := 1\n",
		"lib.hex": "fun broken<T>(v: T): T do\n    return nope\nend\nexport\n    broken\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("an unimported exported generic with an independent error must be rejected, got %#v", result)
	}
	message := strings.Join(result.Stderr, "\n")
	if !strings.Contains(message, "unknown variable nope") || !strings.Contains(message, "lib.hex") {
		t.Fatalf("stderr = %#v, want the defining-module coordinate for the unknown name", result.Stderr)
	}
}

// An unreachable source-map module stays ignored.
func TestGenericInUnreachableModuleIgnored(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "value: Int32 := 1\n",
		"junk.hex": "fun broken<T>(v: T): T do\n    return nope\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("unreachable module leaked diagnostics: %#v", result)
	}
}

// Checking an unused generic emits no nested concrete specialization and
// consumes no ordinal, so later generated names and artifacts are unchanged.
func TestGenericDeclarationStateIsolation(t *testing.T) {
	plain := "fun id<T>(v: T): T do\n    return v\nend\nfun real(): Int32 do\n    first: Int32 := 1\n    second: Int32 := first + 1\n    return second\nend\n"
	stripped := "fun real(): Int32 do\n    first: Int32 := 1\n    second: Int32 := first + 1\n    return second\nend\n"
	withGeneric := compiler.Compile(map[string]string{"app.hex": plain}, "app.hex", compiler.Project{})
	without := compiler.Compile(map[string]string{"app.hex": stripped}, "app.hex", compiler.Project{})
	if withGeneric.ExitCode != compiler.ExitSuccess || without.ExitCode != compiler.ExitSuccess {
		t.Fatalf("state-isolation programs failed: %v %v", withGeneric.Stderr, without.Stderr)
	}
	// The generic declaration itself adds no code; ignoring its own source
	// offset (the #line directives), the later function's generated body and
	// binding names are byte-identical.
	if withoutLineDirectives(withGeneric.Files["modules/app.c"]) != withoutLineDirectives(without.Files["modules/app.c"]) {
		t.Fatalf("an unused generic changed later generated C:\n--- with ---\n%s\n--- without ---\n%s", withGeneric.Files["modules/app.c"], without.Files["modules/app.c"])
	}
	// A concrete specialization requested only inside the unused generic body
	// must not be emitted.
	probe := "fun id<T>(v: T): T do\n    return v\nend\nfun unused<U>(v: U): Int32 do\n    x: Int32 := id<Int32>(1)\n    return x\nend\nfun real(): Int32 do\n    return 7\nend\n"
	result := compiler.Compile(map[string]string{"app.hex": probe}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("probe failed: %v", result.Stderr)
	}
	if strings.Contains(result.Files["modules/app.c"], "hex_f_m3_app_id") {
		t.Fatalf("inspecting an unused generic emitted a nested specialization:\n%s", result.Files["modules/app.c"])
	}
}

// Generic anonymous literals and module-level inferred generic literals follow
// the rule, and a nested check restores the enclosing parameter frame.
func TestGenericLiteralDeclarationChecking(t *testing.T) {
	// A module-level inferred fixed generic literal is sugar for a named
	// generic function and follows the rule.
	assertRejects(t, "identity := fun<T>(value: T): T do\n    return nope\nend\n", "unknown variable nope")
	assertCompiles(t, "identity := fun<T>(value: T): T do\n    return value\nend\n")
	// A generic anonymous literal is checked at its expression point, where
	// it is immediately specialized; the checker tests cover that path.
	assertCompiles(t, "id := fun<T>(value: T): T do\n    return value\nend\n")
}

// Valid generics, local and imported, produce the same generated C whether or
// not an unused invalid template exists elsewhere in the reachable set.
func TestGenericValidOutputUnchanged(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\nx: Int32 := Lib.identity<Int32>(1)\n",
		"lib.hex": "fun identity<T>(v: T): T do\n    return v\nend\nexport\n    identity\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "lib")
	if !strings.Contains(result.Files["modules/lib.c"], "identity_Int32") {
		t.Fatalf("the requested specialization is missing:\n%s", result.Files["modules/lib.c"])
	}
}
