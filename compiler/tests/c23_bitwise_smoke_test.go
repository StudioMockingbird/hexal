//go:build c23

package tests

import "testing"

// Smoke-check that RFC 0032 bit operations generate C that gcc accepts:
// bitwise ops, defined shifts, memcpy bit casts, and endian conversion.
func TestGeneratedBitwiseCCompiles(t *testing.T) {
	source := "fun demo()\n    mut flags: UInt32 = 0xFFFF0000\n    masked: UInt32 = flags & 0x00FF\n    combined: UInt32 = masked | 0xF0\n    xor: UInt32 = combined ^ 0x0F0F\n    complement: UInt8 = ~0x0F\n    shifted: UInt32 = flags << 4\n    back: UInt32 = shifted >> 8\n    mut signed: Int8 = 64\n    wrapped: Int8 = signed << 1\n    mut negative: Int8 = -4\n    halved: Int8 = negative >> 1\n    floating: Float64 = 1.5\n    bits: UInt64 = floating.bit_cast<UInt64>()\n    again: Float64 = bits.bit_cast<Float64>()\n    value: UInt32 = 0x01020304\n    little: Array<UInt8, 4> = value.to_le_bytes()\n    big: Array<UInt8, 4> = value.to_be_bytes()\n    from_little: UInt32 = UInt32.from_le_bytes(little)\n    from_big: UInt32 = UInt32.from_be_bytes(big)\n    mut signed16: Int16 = -2\n    signed_little: Array<UInt8, 2> = signed16.to_le_bytes()\n    signed_back: Int16 = Int16.from_le_bytes(signed_little)\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}
