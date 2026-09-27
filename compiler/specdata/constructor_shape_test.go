package specdata

import "testing"

func TestListConstructorResolvesByParameterShape(t *testing.T) {
	allocated, allocatedOK := TypeConstructorByShape("List", []ParamKind{ParamType})
	inline, inlineOK := TypeConstructorByShape("List", []ParamKind{ParamType, ParamInteger})
	if !allocatedOK || allocated.ID != TypeList {
		t.Fatalf("allocated List constructor = %#v, %v", allocated, allocatedOK)
	}
	if !inlineOK || inline.ID != TypeInlineList {
		t.Fatalf("inline List constructor = %#v, %v", inline, inlineOK)
	}
	if _, ok := TypeConstructorByShape("List", []ParamKind{ParamInteger, ParamType}); ok {
		t.Fatal("List constructor resolved an unsupported parameter shape")
	}
}
