package types

import "testing"

// TestForeignRecordMembersReturnsACopy pins that the exported getter hands back
// a copy: mutating the returned slice must not reach the program-wide record,
// which every later reader shares.
func TestForeignRecordMembersReturnsACopy(t *testing.T) {
	arena := NewArena()
	record, _ := arena.ForeignRecord("k", "Point", "point", true, 1, 1)
	record = arena.CompleteForeignRecord(record, []ObjectMember{
		{Name: "x", Type: Int32},
		{Name: "y", Type: Int32},
	})

	first := ForeignRecordMembers(record)
	if len(first) != 2 {
		t.Fatalf("members = %d, want 2", len(first))
	}
	first[0] = ObjectMember{Name: "clobbered", Type: Int32}

	again := ForeignRecordMembers(record)
	if len(again) != 2 || again[0].Name != "x" {
		t.Fatalf("stored record mutated through the returned slice: %+v", again)
	}
}
