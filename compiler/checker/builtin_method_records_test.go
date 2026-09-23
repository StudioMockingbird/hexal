package checker

import (
	"testing"

	"hexal/compiler/specdata"
)

// The built-in method families must read specdata.Method rather than restate
// which methods exist. Each program below compiles with the real registry;
// replacing the lookup with an empty registry must move the answer to the same
// family's no-method diagnostic. That is the difference between a record the
// consumer reads and one that merely decorates a handwritten switch.
func TestBuiltinMethodDispatchReadsTheRegistry(t *testing.T) {
	cases := []struct {
		family string
		source string
		want   string
	}{
		{
			family: "List",
			source: "let h: Heap = Heap()\nlet values: List<Int32> = List<Int32>(h)\nvalues.push(1)\n",
			want:   "List<Int32> has no method push",
		},
		{
			family: "Dict",
			source: "let h: Heap = Heap()\nlet scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\nscores.insert(1, 2)\n",
			want:   "Dict<Int32, Int32> has no method insert",
		},
		{
			family: "Atomic",
			source: "let counter: Atomic<Int32> = Atomic<Int32>(0)\nlet loaded: Int32 = counter.load()\n",
			want:   "Atomic<Int32> has no method named load",
		},
		{
			family: "Stash",
			source: "type Node is struct value: Int32 end\nlet stash = Stash<Node>()\nlet node: Ptr<mut Node> = stash.allocate(Node(value = 1))\n",
			want:   "Stash has no method allocate; use allocate, reset, or destroy",
		},
		{
			family: "Pool",
			source: "type Node is struct value: Int32 end\nlet pool = Pool<Node>(4)\npool.destroy()\n",
			want:   "Pool has no method destroy; use allocate, free, or destroy",
		},
		{
			family: "Array",
			source: "let mut data: Array<Int32, 2> = [1, 2]\nlet view: Slice<Int32> = data.slice(0, 1)\n",
			want:   "Array<Int32, 2> has no method slice",
		},
		{
			family: "Slice",
			source: "fun demo(view: Slice<Int32>) do\n    unsafe do\n        let raw: Ptr<Int32> | Nil = view.pointer()\n    end\nend\n",
			want:   "Slice<Int32> has no method pointer",
		},
		{
			family: "String",
			source: "let text: String = \"hi\"\nlet raw: Slice<UInt8> = text.bytes()\n",
			want:   "String has no method bytes",
		},
		{
			family: "Channel",
			source: "fun demo(): Int32 | Error do\n    let h: Heap = Heap()\n    let channel: Channel<Int32> = try Channel<Int32>(h, 1)\n    channel.close()\n    return 0\nend\n",
			want:   "Channel has no method close; use send, receive, close, length, capacity, is_closed, or free",
		},
		{
			family: "Mutex",
			source: "fun demo(): Int32 | Error do\n    let h: Heap = Heap()\n    let mutex: Mutex = try Mutex(h)\n    mutex.lock()\n    return 0\nend\n",
			want:   "Mutex has no method lock; use lock, unlock, or free",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.family, func(t *testing.T) {
			// The real registry accepts the program, so the method genuinely
			// exists on this receiver today.
			requireAccepted(t, testCase.source)

			original := builtinMethod
			builtinMethod = func(specdata.TypePattern, string) (specdata.MethodSpec, bool) {
				return specdata.MethodSpec{}, false
			}
			defer func() { builtinMethod = original }()

			requireDiagnostic(t, testCase.source, testCase.want)
		})
	}
}

// TestPerFamilyDispatchMatchesTheRegistry pins each family's accepted names to
// the registry's declared set, so a family that keeps an extra name or drops
// one fails here rather than at runtime through a silent acceptance.
func TestPerFamilyDispatchMatchesTheRegistry(t *testing.T) {
	declared := make(map[string]map[string]bool)
	for _, method := range specdata.Methods() {
		owner := method.Owner.Constructor
		if owner == "" {
			owner = method.Owner.Exact
		}
		if declared[string(owner)] == nil {
			declared[string(owner)] = make(map[string]bool)
		}
		declared[string(owner)][method.Name] = true
	}
	for owner, names := range map[string][]string{
		"Array":   {"length", "slice", "mut_slice"},
		"Slice":   {"length", "slice", "pointer"},
		"List":    {"length", "slice", "mut_slice", "push", "clear", "pop", "free"},
		"Dict":    {"length", "insert", "get", "find", "remove", "contains", "free"},
		"Task":    {"join", "detach"},
		"Channel": {"send", "receive", "close", "free", "length", "capacity", "is_closed"},
		"Atomic":  {"load", "store", "exchange", "fetch_add", "fetch_sub", "compare_exchange"},
		"Stash":   {"allocate", "reset", "destroy"},
		"Pool":    {"allocate", "free", "destroy"},
		"Mutex":   {"lock", "unlock", "free"},
		"String":  {"length", "rune_length", "grapheme_length", "byte_cursor", "rune_cursor", "grapheme_cursor", "bytes", "slice", "casefold", "normalize", "copy", "concat", "free", "c_pointer"},
	} {
		for _, name := range names {
			if !declared[owner][name] {
				t.Errorf("dispatch accepts %s.%s but the registry declares no such method", owner, name)
			}
		}
	}
}
