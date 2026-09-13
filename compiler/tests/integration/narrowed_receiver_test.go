package integration

import (
	"strings"
	"testing"
)

// A union binding narrowed to one member reads through that member's payload
// wherever it is a receiver: built-in handle methods, compiler conversions,
// and object member selection.
func TestNarrowedUnionReceiversReadPayload(t *testing.T) {
	result := assertCompiles(t, "fun sq(v: Int32): Int32 do\n    return v * v\nend\n"+
		"fun run(): Int64 do\n"+
		"    t: Task<Int32> | Error := spawn sq(3)\n"+
		"    if t is Task<Int32> then\n"+
		"        joined: Int32 := t.join()\n"+
		"    end\n"+
		"    x: Int32 | Error := 3\n"+
		"    if x is Int32 then\n"+
		"        return x.to<Int64>()\n"+
		"    end\n"+
		"    e: IO | Error := IO.stdin()\n"+
		"    if e is Error then\n"+
		"        print(e.header)\n"+
		"    end\n"+
		"    return 0\n"+
		"end\n")
	body := withoutLineDirectives(rootC(t, result))
	for _, required := range []string{"hex_task_join_Int32(hex_v_t.payload.", "hex_v_x.payload.", "hex_v_e.payload."} {
		if !strings.Contains(body, required) {
			t.Fatalf("narrowed receiver must read its payload (%s):\n%s", required, body)
		}
	}
	for _, forbidden := range []string{"hex_task_join_Int32(hex_v_t)", "(int64_t)hex_v_x;"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("narrowed receiver reads the whole union (%s):\n%s", forbidden, body)
		}
	}
}

// A try whose success union keeps a tag-only member (EoS or Nil) rebuilds
// that member without a payload copy.
func TestTrySuccessUnionWithTagOnlyMember(t *testing.T) {
	result := assertCompiles(t, "fun run(h: Heap): Nil | Error do\n"+
		"    buffer: List<Byte> := List<Byte>(h)\n"+
		"    defer buffer.free(h)\n"+
		"    mut stream: Bytes := Bytes.over(buffer)\n"+
		"    got := try stream.read(buffer, 4)\n"+
		"    return nil\n"+
		"end\n"+
		"fun maybe(): Int32 | Nil | Error do\n    return nil\nend\n"+
		"fun g(): Nil | Error do\n    v := try maybe()\n    return nil\nend\n")
	body := withoutLineDirectives(rootC(t, result))
	if strings.Contains(body, ".payload.hex_m_EoS") || strings.Contains(body, ".payload.hex_m_Nil") {
		t.Fatalf("try must not copy a tag-only payload:\n%s", body)
	}
}
