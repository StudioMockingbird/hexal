package integration

// Stash<T> and Pool<T>: independent typed allocators built directly on the
// default Heap primitives.

import (
	"strings"
	"testing"
)

func TestStashAllocateResetDestroyCompiles(t *testing.T) {
	result := assertCompiles(t, "type Node = { value: Int32, }\n"+
		"fun demo() do\n"+
		"    stash := Stash<Node>.new()\n"+
		"    defer stash.destroy()\n"+
		"    node: MutPtr<Node> := stash.allocate(Node { value = 1 })\n"+
		"    stash.reset()\n"+
		"    node2: MutPtr<Node> := stash.allocate(Node { value = 2 })\n"+
		"end")
	header := rootH(t, result)
	for _, want := range []string{"hex_stash_new_hex_t_m3_app_Node", "hex_stash_alloc_hex_t_m3_app_Node"} {
		if !strings.Contains(header, want) {
			t.Fatalf("missing %s:\n%s", want, header)
		}
	}
	body := rootC(t, result)
	for _, want := range []string{"hex_stash_reset(", "hex_stash_destroy("} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s:\n%s", want, body)
		}
	}
}

func TestStashUnionElementAllocateCompiles(t *testing.T) {
	result := assertCompiles(t, "type Edge = { weight: Int32, }\n"+
		"type Node = { value: Int32, }\n"+
		"type Item = Node | Edge\n"+
		"fun demo() do\n"+
		"    stash := Stash<Item>.new()\n"+
		"    defer stash.destroy()\n"+
		"    item: MutPtr<Item> := stash.allocate(Node { value = 1 })\n"+
		"end")
	if result.ExitCode != 0 {
		t.Fatalf("expected success")
	}
}

func TestPoolAllocateFreeDestroyCompiles(t *testing.T) {
	result := assertCompiles(t, "type Node = { value: Int32, }\n"+
		"fun demo() do\n"+
		"    pool := Pool<Node>.new(4)\n"+
		"    defer pool.destroy()\n"+
		"    node: MutPtr<Node> := pool.allocate(Node { value = 1 })\n"+
		"    pool.free(node)\n"+
		"end")
	header := rootH(t, result)
	for _, want := range []string{"hex_pool_new_Node", "hex_pool_alloc_Node", "hex_pool_free_Node", "hex_pool_destroy_Node", "free_stack", "live"} {
		if !strings.Contains(header, want) {
			t.Fatalf("missing %s:\n%s", want, header)
		}
	}
}

func TestStashPoolRejections(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			"individual free rejected",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    stash := Stash<Node>.new()\n" +
				"    node: MutPtr<Node> := stash.allocate(Node { value = 1 })\n" +
				"    stash.free(node)\n" +
				"end",
			"Stash allocations are released by reset or destroy",
		},
		{
			"explicit allocate type argument rejected",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    stash := Stash<Node>.new()\n" +
				"    node: MutPtr<Node> := stash.allocate<Node>(Node { value = 1 })\n" +
				"end",
			"Stash allocation accepts no type arguments",
		},
		{
			"constant zero Pool capacity rejected",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    pool := Pool<Node>.new(0)\n" +
				"end",
			"Pool capacity must be positive",
		},
		{
			"Pool destroy with live tracked slot rejected",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    pool := Pool<Node>.new(4)\n" +
				"    node: MutPtr<Node> := pool.allocate(Node { value = 1 })\n" +
				"    pool.destroy()\n" +
				"end",
			"Pool cannot be destroyed while a locally tracked slot is live",
		},
		{
			"use after Stash destroy rejected",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    stash := Stash<Node>.new()\n" +
				"    stash.destroy()\n" +
				"    node: MutPtr<Node> := stash.allocate(Node { value = 1 })\n" +
				"end",
			"released on every path",
		},
		{
			"use of allocation after Stash reset rejected",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    stash := Stash<Node>.new()\n" +
				"    node: MutPtr<Node> := stash.allocate(Node { value = 1 })\n" +
				"    stash.reset()\n" +
				"    node.value = 5\n" +
				"end",
			"released on every path",
		},
		{
			"Pool double destroy rejected",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    pool := Pool<Node>.new(4)\n" +
				"    pool.destroy()\n" +
				"    pool.destroy()\n" +
				"end",
			"released on every path",
		},
		{
			"List.new rejects a Stash allocator",
			"type Node = { value: Int32, }\n" +
				"fun demo() do\n" +
				"    stash := Stash<Node>.new()\n" +
				"    list := List<Node>.new(stash)\n" +
				"end",
			"List<T>.new requires a Heap",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, testCase.want)
		})
	}
}
