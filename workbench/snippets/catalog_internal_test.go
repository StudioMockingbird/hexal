package snippets

import (
	"strings"
	"testing"
)

// validate and lineLimitWarnings are catalog-internal contracts; their direct
// coverage lives beside them. Public-API compilation stays in snippets_test.
func TestValidateRejectsDuplicateSnippetID(t *testing.T) {
	categories := []Category{
		{ID: "values", Name: "Values", Snippets: []Snippet{{ID: "one", Name: "One", Entrypoint: "app.hex", Sources: map[string]string{"app.hex": "value: Int32 := 1\n"}}}},
		{ID: "text", Name: "Text", Snippets: []Snippet{{ID: "one", Name: "One", Entrypoint: "app.hex", Sources: map[string]string{"app.hex": "label: Strand := \"a\"\n"}}}},
	}
	if err := validate(categories); err == nil || !strings.Contains(err.Error(), "duplicate snippet") {
		t.Fatalf("validate() = %v, want a duplicate-snippet failure", err)
	}
}

func TestLineLimitWarningsReportsLongSnippets(t *testing.T) {
	long := strings.Repeat("value = value\n", 21)
	categories := []Category{
		{ID: "values", Name: "Values", Snippets: []Snippet{
			{ID: "short", Name: "Short", Sources: map[string]string{"app.hex": "value: Int32 := 1\n"}},
			{ID: "long", Name: "Long", Sources: map[string]string{"app.hex": long}},
		}},
	}
	warnings := lineLimitWarnings(categories)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "values/long") {
		t.Fatalf("lineLimitWarnings() = %v, want exactly the values/long warning", warnings)
	}
}
