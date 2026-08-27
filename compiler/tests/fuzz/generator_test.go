package fuzz

import "fmt"

// generatedProgram is one seed's deterministic candidate: a two-module Hexal
// program covering every construct in constructChecklist (tier2_test.go).
// The seed only rotates a concrete type and an ADT variant, so every
// candidate is expected to compile; nothing requires the generator to
// manufacture rejections.
type generatedProgram struct {
	seed       uint64
	sources    map[string]string
	entrypoint string
}

// rotatingTypes is the small, hand-verified set of concrete types the
// generator rotates through for its generic specialization. This is not the
// full language: this generator is scoped to the constructs where canonical
// identity matters, not type-system breadth.
var rotatingTypes = []string{"Int32", "Int64", "Float64", "Bool"}

// rotatingLiteral returns one literal of typ, valid as an argument to
// GenIdentity<typ>.
func rotatingLiteral(typ string) string {
	switch typ {
	case "Bool":
		return "true"
	case "Float64":
		return "1.5"
	default:
		return "3"
	}
}

// generateProgram deterministically builds the seed-th candidate. The base
// template is fixed -- an imported object and generic function, a local ADT
// matched over both variants, a local union, and a constructed List -- so
// every checklist construct is present in every candidate; the seed rotates
// which concrete type specializes GenIdentity and which ADT variant is
// constructed, both purely as a function of seed with no other input.
func generateProgram(seed uint64) generatedProgram {
	genericType := rotatingTypes[int(seed%uint64(len(rotatingTypes)))]
	literal := rotatingLiteral(genericType)

	signalConstruct := "GenSignal.GenAlpha"
	if seed%2 == 1 {
		signalConstruct = "GenSignal.GenBeta { level = 7 }"
	}

	app := fmt.Sprintf(
		"module Lib = import \"./lib\"\n"+
			"type GenSignal as | GenAlpha | GenBeta { level: Int32 } end\n"+
			"fun run(h: Heap): Int32 do\n"+
			"    point: Lib.GenPoint := Lib.GenMakePoint()\n"+
			"    signal: GenSignal := %s\n"+
			"    label: Int32 := match signal is\n"+
			"    | GenSignal.GenAlpha then 0\n"+
			"    | GenSignal.GenBeta then signal.level\n"+
			"    end\n"+
			"    numbers: List<Int32> := List<Int32>.new(h)\n"+
			"    defer numbers.free(h)\n"+
			"    numbers.push(point.x + label)\n"+
			"    value: %s := Lib.GenIdentity<%s>(%s)\n"+
			"    maybe: Int32 | Nil := label\n"+
			"    return numbers.length().to<Int32>()\n"+
			"end\n"+
			"h: Heap := Heap.new()\n"+
			"total: Int32 := run(h)\n",
		signalConstruct, genericType, genericType, literal,
	)
	lib := "export type GenPoint = { x: Int32, y: Int32 }\n" +
		"export fun GenMakePoint(): GenPoint do\n" +
		"    return GenPoint { x = 1, y = 2 }\n" +
		"end\n" +
		"export fun GenIdentity<T>(value: T): T do\n" +
		"    return value\n" +
		"end\n"

	return generatedProgram{
		seed:       seed,
		sources:    map[string]string{"app.hex": app, "lib.hex": lib},
		entrypoint: "app.hex",
	}
}
