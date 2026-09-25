package generator

import (
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The evaluation-order gate must classify every checked expression kind, so
// a newly added kind fails here until someone decides whether a bare node of
// that kind observes an effect of its own. "Observes" means the kind itself
// may read or write state the surrounding evaluation order can observe (a
// call, a mutation, an allocation); a kind whose children observe is handled
// by expressionMayObserve's own child walk regardless of this classification.
// The behavioral sweep relies on operands.go being a plain iota, which
// expressionKindConstants verifies, so source position equals constant value.
func TestObservabilityClassifiesEveryExpressionKind(t *testing.T) {
	alwaysObserving := map[string]bool{
		"CallExpression":                  true,
		"MethodCallExpression":            true,
		"StringFromBytesExpression":       true,
		"StringFromRunesExpression":       true,
		"StringInterpolateExpression":     true,
		"InlineStringConstructExpression": true,
		"TextCoerceExpression":            true,
		"ListNewExpression":               true,
		"DictNewExpression":               true,
		"TryExpression":                   true,
		"PrintExpression":                 true,
		"SpawnExpression":                 true,
		"TaskYieldExpression":             true,
		"TaskMethodCallExpression":        true,
		"ChannelConstructorExpression":    true,
		"MutexConstructorExpression":      true,
		"MutexMethodCallExpression":       true,
		"AtomicConstructorExpression":     true,
		"AtomicMethodCallExpression":      true,
		"StashConstructorExpression":      true,
		"StashMethodCallExpression":       true,
		"PoolConstructorExpression":       true,
		"PoolMethodCallExpression":        true,
		"HeapAllocateExpression":          true,
		"HeapAllocateAlignedExpression":   true,
		"HeapFreeExpression":              true,
		"VolatileReadExpression":          true,
		"VolatileWriteExpression":         true,
		"StreamConstructorExpression":     true,
		"StreamMethodCallExpression":      true,
		"TimeExpression":                  true,
		"NetworkExpression":               true,
		"CorelibCallExpression":           true,
		"MatchExpression":                 true,
	}
	// nameDependent kinds sit in expressionMayObserve's switch but their bare
	// answer depends on Name (and, for a cursor peek, the receiver type).
	nameDependent := map[string]bool{
		"CollectionMethodCallExpression": true,
		"StringMethodCallExpression":     true,
		"ChannelMethodCallExpression":    true,
		"CursorMethodCallExpression":     true,
	}
	neverObservesAlone := map[string]bool{
		"VariableExpression":                 true,
		"AddressOfExpression":                true,
		"DereferenceExpression":              true,
		"MemberExpression":                   true,
		"ObjectExpression":                   true,
		"ConstantExpression":                 true,
		"UnaryOperationExpression":           true,
		"BinaryOperationExpression":          true,
		"FunctionReferenceExpression":        true,
		"ForeignFunctionReferenceExpression": true,
		"ForeignConstantExpression":          true,
		"ForeignGlobalExpression":            true,
		"NilExpression":                      true,
		"EosExpression":                      true,
		"NullTestExpression":                 true,
		"UnionInjectionExpression":           true,
		"UnionWidenExpression":               true,
		"UnionTestExpression":                true,
		"UnionPayloadExpression":             true,
		"UnionEqualityExpression":            true,
		"AdtConstructExpression":             true,
		"AdtPayloadExpression":               true,
		"ArrayLiteralExpression":             true,
		"IndexExpression":                    true,
		"CollectionSliceExpression":          true,
		"StringLiteralExpression":            true,
		"BitCastExpression":                  true,
		"EndianConversionExpression":         true,
		"DeepEqualityExpression":             true,
		"StringCompareExpression":            true,
		"WideningExpression":                 true,
		"ConversionExpression":               true,
		"RuneMethodCallExpression":           true,
		"GraphemeMethodCallExpression":       true,
		"PointerOffsetExpression":            true,
		"PointerIndexExpression":             true,
		"PointerCastExpression":              true,
		"SliceBridgeExpression":              true,
		"BytesOverExpression":                true,
		"LayoutExpression":                   true,
		"FunctionLiteralExpression":          true,
		"ErrorHeaderExpression":              true,
		"ErrorKindHeaderExpression":          true,
		"ModuleValueExpression":              true,
	}

	kinds := expressionKindConstants(t)
	switchKinds := dispatchedExpressionKinds(t, functionFile(t, "expressionMayObserve"), "expressionMayObserve")

	classified := make(map[string]string)
	claim := func(group string, set map[string]bool) {
		for kind := range set {
			if groupBy, claimed := classified[kind]; claimed {
				t.Errorf("expression kind %s is classified by both %s and %s", kind, groupBy, group)
			}
			classified[kind] = group
		}
	}
	claim("always-observing", alwaysObserving)
	claim("name-dependent", nameDependent)
	claim("never-observing-alone", neverObservesAlone)

	for _, kind := range kinds {
		if kind == "InvalidExpression" {
			// The sole intentional fall-through: a valid checked program
			// never produces it, and both primary dispatchers fail closed.
			if group, claimed := classified[kind]; claimed || switchKinds[kind] {
				t.Errorf("expression kind %s must stay unclassified, found classified as %q or cased in the switch", kind, group)
			}
			continue
		}
		group, claimed := classified[kind]
		if !claimed {
			t.Errorf("expression kind %s is unclassified; add it to expressionMayObserve's switch or to the never-observing set with a reason", kind)
			continue
		}
		wantSwitch := alwaysObserving[kind] || nameDependent[kind]
		if switchKinds[kind] != wantSwitch {
			t.Errorf("expression kind %s is classified %s but its switch membership is %v", kind, group, switchKinds[kind])
		}
	}
	for kind := range switchKinds {
		if group, claimed := classified[kind]; !claimed {
			t.Errorf("expressionMayObserve cases %s, which the classification table does not know", kind)
		} else if group == "never-observing-alone" {
			t.Errorf("expressionMayObserve cases %s, which the classification table marks never-observing-alone", kind)
		}
	}

	for index, kind := range kinds {
		if kind == "InvalidExpression" || nameDependent[kind] {
			continue
		}
		bare := &checker.Expression{Kind: checker.ExpressionKind(index)}
		if got, want := expressionMayObserve(bare, nil), alwaysObserving[kind]; got != want {
			t.Errorf("bare %s observes = %v, want %v (source position %d)", kind, got, want, index)
		}
	}

	nameCases := []struct {
		desc string
		node *checker.Expression
		want bool
	}{
		{"collection push mutates the receiver", &checker.Expression{Kind: checker.CollectionMethodCallExpression, Name: "push"}, true},
		{"collection length reads a copy", &checker.Expression{Kind: checker.CollectionMethodCallExpression, Name: "length"}, false},
		{"string concat allocates", &checker.Expression{Kind: checker.StringMethodCallExpression, Name: "concat"}, true},
		{"string length reads a copy", &checker.Expression{Kind: checker.StringMethodCallExpression, Name: "length"}, false},
		{"channel send observes", &checker.Expression{Kind: checker.ChannelMethodCallExpression, Name: "send"}, true},
		{"channel capacity reads a copy", &checker.Expression{Kind: checker.ChannelMethodCallExpression, Name: "capacity"}, false},
		{"cursor next advances every cursor binding", &checker.Expression{Kind: checker.CursorMethodCallExpression, Name: "next"}, true},
		{"byte cursor peek reads by value", &checker.Expression{Kind: checker.CursorMethodCallExpression, Name: "peek", OperandType: compilerTypes.ByteCursorType}, false},
		{"grapheme cursor peek fills the scan cache", &checker.Expression{Kind: checker.CursorMethodCallExpression, Name: "peek", OperandType: compilerTypes.GraphemeCursorType}, true},
		{"cursor has_next reads a copy", &checker.Expression{Kind: checker.CursorMethodCallExpression, Name: "has_next"}, false},
		{"cursor offset reads a copy", &checker.Expression{Kind: checker.CursorMethodCallExpression, Name: "offset"}, false},
	}
	for _, testCase := range nameCases {
		if got := expressionMayObserve(testCase.node, nil); got != testCase.want {
			t.Errorf("%s observes = %v, want %v", testCase.desc, got, testCase.want)
		}
	}
}
