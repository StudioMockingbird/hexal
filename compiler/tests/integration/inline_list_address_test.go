package integration

import (
	"strings"
	"testing"
)

func TestAddressUsesCanonicalInlineByteListsAndChecksDynamicLength(t *testing.T) {
	source := "import\n    Net from std.net\nend\n" +
		"fun make_address(bytes: List<Byte, 4>): Net.Address do\n" +
		"    return Net.Address.IPv4(bytes = bytes, port = 80)\nend\n"
	result := assertCompiles(t, source)
	module := rootC(t, result)
	if !strings.Contains(module, "hex_list_inline_check_address_bytes_UInt8_4(hex_v_bytes)") {
		t.Fatalf("dynamic Address payload lacks its exact-length check:\n%s", module)
	}
	if !strings.Contains(listH(t, result), "network address byte length does not match address family") {
		t.Fatal("inline byte-list helper lacks the Address length trap")
	}

	wrongLiteral := "import\n    Net from std.net\nend\n" +
		"let address: Net.Address = Net.Address.IPv4(bytes = [b'a', b'b'], port = 80)\n"
	failed := compileSource(wrongLiteral)
	if failed.ExitCode == 0 || len(failed.Stderr) == 0 || !strings.Contains(failed.Stderr[0], "Address.IPv4 bytes literal requires 4 elements; got 2") {
		t.Fatalf("wrong Address literal stderr = %#v", failed.Stderr)
	}

	correctLiteral := "import\n    Net from std.net\nend\n" +
		"let address: Net.Address = Net.Address.IPv4(bytes = [b'a', b'b', b'c', b'd'], port = 80)\n"
	assertCompiles(t, correctLiteral)
}
