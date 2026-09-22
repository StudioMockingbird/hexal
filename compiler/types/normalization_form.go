package types

// NormalizationForm is the protected builtin selector for String.normalize:
// the four Unicode normalization forms. Every variant is a unit variant, and
// the set is fixed by the Unicode standard, so a type-mode match over it is
// exhaustive without a final else.

// NormalizationFormVariantNames is the fixed variant list; its order is the
// order the runtime maps onto utf8proc options.
var NormalizationFormVariantNames = []string{"NFC", "NFD", "NFKC", "NFKD"}

// NormalizationFormType is the protected compiler-owned NormalizationForm ADT.
// It is registered as a builtin ADT beside ErrorKind: its struct is
// hand-written in hexal/string.h rather than generated.
var NormalizationFormType = normalizationFormType()

func normalizationFormType() Type {
	variants := make([]AdtVariant, len(NormalizationFormVariantNames))
	for index, name := range NormalizationFormVariantNames {
		variants[index] = AdtVariant{Name: name}
	}
	adt := &AdtType{
		Name:     "NormalizationForm",
		CName:    "hex_t_NormalizationForm",
		Variants: variants,
		identity: newTypeIdentity(),
	}
	return Type{
		Name:         "NormalizationForm",
		CName:        adt.CName,
		CanonicalKey: canonicalNominalKey("NormalizationForm", ""),
		Adt:          adt,
		identity:     adt.identity,
	}
}

func init() {
	builtinTypes["NormalizationForm"] = NormalizationFormType
}

// IsNormalizationForm reports whether typ is the canonical NormalizationForm
// ADT.
func IsNormalizationForm(typ Type) bool {
	return typ.Adt != nil && typ.Adt == NormalizationFormType.Adt
}
