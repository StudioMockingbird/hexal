package specdata

import "testing"

// validateErrorKinds rejects a registry that cannot be trusted: an empty or
// repeated identity, a payload count other than exactly one, a variant that
// declares both unit and payload facts or neither, and a payload header type
// that names no declared constructor.
func TestValidateErrorKindsRejectsMalformedRegistry(t *testing.T) {
	original := errorKinds
	defer func() { errorKinds = original }()
	for _, testCase := range []struct {
		name  string
		specs []ErrorKindSpec
	}{
		{"empty identity", []ErrorKindSpec{{Unit: true}, {ID: ErrorKindOther, HeaderType: TypeInlineString}}},
		{"repeated identity", []ErrorKindSpec{{ID: ErrorKindOther, HeaderType: TypeInlineString}, {ID: ErrorKindOther, Unit: true}}},
		{"no payload", []ErrorKindSpec{{ID: ErrorKindNotFound, Unit: true}, {ID: ErrorKindOther, Unit: true}}},
		{"two payloads", []ErrorKindSpec{{ID: ErrorKindOther, HeaderType: TypeInlineString}, {ID: ErrorKindNotFound, HeaderType: TypeInlineString}}},
		{"unit with a payload header", []ErrorKindSpec{{ID: ErrorKindOther, Unit: true, HeaderType: TypeInlineString}}},
		{"payload without a header type", []ErrorKindSpec{{ID: ErrorKindOther}}},
		{"undeclared header constructor", []ErrorKindSpec{{ID: ErrorKindOther, HeaderType: TypeID("Missing")}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			errorKinds = testCase.specs
			if err := validateErrorKinds(); err == nil {
				t.Fatalf("validateErrorKinds() = nil, want a rejection for %s", testCase.name)
			}
		})
	}
}
