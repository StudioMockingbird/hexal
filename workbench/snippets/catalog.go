// Package snippets owns the reusable Hexal example corpus used by the
// workbench and available to compiler smoke tests.
package snippets

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

//go:embed categories/*.json
var categoryFiles embed.FS

// Category groups snippets with related language concepts.
type Category struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Snippets []Snippet `json:"snippets"`
}

// Snippet is one meaningful in-memory Hexal program. Sources supports
// multi-module programs without introducing filesystem behavior.
type Snippet struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Entrypoint    string            `json:"entrypoint"`
	Sources       map[string]string `json:"sources"`
	Features      []string          `json:"features"`
	ReservedWords []string          `json:"reservedWords,omitempty"`
}

type diskCategory struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Snippets []diskSnippet `json:"snippets"`
}

type diskSnippet struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Entrypoint    string              `json:"entrypoint"`
	Sources       map[string][]string `json:"sources"`
	Features      []string            `json:"features"`
	ReservedWords []string            `json:"reservedWords,omitempty"`
}

// RequiredReservedWords mirrors the canonical reserved-word grammar. Catalog
// validation makes omissions visible whenever that grammar grows.
var RequiredReservedWords = []string{
	"true", "false", "nil", "eos", "mut", "ref", "type", "and", "or", "is",
	"fun", "impl", "end", "return", "if", "elseif", "else", "while", "break",
	"continue", "defer", "try", "errdefer", "spawn", "as", "match", "then",
	"self", "for", "in", "do", "module", "import", "export",
}

// RequiredFeatures is the workbench's explicit feature-coverage contract.
// Each entry must be demonstrated by at least one small program.
var RequiredFeatures = []string{
	"comments", "bindings", "scalar-types", "literals", "aliases", "objects",
	"copying", "pointers", "nullability", "functions", "no-result-functions",
	"function-values", "methods", "generics", "unions", "adts", "match",
	"lossless-widening", "numeric-conversions", "arithmetic", "bitwise", "equality-ordering",
	"bit-casting", "endian-conversion", "truthiness", "if-elseif-else", "while", "for", "defer", "errors",
	"try-errdefer", "heap-allocation", "arrays", "views", "view-pointer-bridge",
	"lists", "dicts", "text", "print", "tasks",
	"channels", "mutex", "atomics", "layout", "volatile", "unknown-pointers", "modules", "exports",
}

// Load returns the embedded category files in lexical filename order.
func Load() ([]Category, error) {
	paths, err := fs.Glob(categoryFiles, "categories/*.json")
	if err != nil {
		return nil, fmt.Errorf("list snippet categories: %w", err)
	}
	slices.Sort(paths)

	categories := make([]Category, 0, len(paths))
	for _, path := range paths {
		data, err := categoryFiles.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var disk diskCategory
		if err := json.Unmarshal(data, &disk); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		category := Category{ID: disk.ID, Name: disk.Name, Snippets: make([]Snippet, 0, len(disk.Snippets))}
		for _, item := range disk.Snippets {
			sources := make(map[string]string, len(item.Sources))
			for name, lines := range item.Sources {
				sources[name] = strings.Join(lines, "\n") + "\n"
			}
			category.Snippets = append(category.Snippets, Snippet{
				ID: item.ID, Name: item.Name, Description: item.Description,
				Entrypoint: item.Entrypoint, Sources: sources,
				Features: item.Features, ReservedWords: item.ReservedWords,
			})
		}
		categories = append(categories, category)
	}
	if err := Validate(categories); err != nil {
		return nil, err
	}
	return categories, nil
}

// Validate enforces identity, source, feature, and reserved-word coverage.
func Validate(categories []Category) error {
	categoryIDs := map[string]bool{}
	snippetIDs := map[string]bool{}
	features := map[string]bool{}
	words := map[string]bool{}
	for _, category := range categories {
		if category.ID == "" || category.Name == "" || categoryIDs[category.ID] {
			return fmt.Errorf("invalid or duplicate snippet category %q", category.ID)
		}
		categoryIDs[category.ID] = true
		if len(category.Snippets) == 0 {
			return fmt.Errorf("snippet category %q is empty", category.ID)
		}
		for _, snippet := range category.Snippets {
			if snippet.ID == "" || snippet.Name == "" || snippetIDs[snippet.ID] {
				return fmt.Errorf("invalid or duplicate snippet %q", snippet.ID)
			}
			snippetIDs[snippet.ID] = true
			if snippet.Entrypoint == "" || snippet.Sources[snippet.Entrypoint] == "" {
				return fmt.Errorf("snippet %q has no entrypoint source", snippet.ID)
			}
			for _, feature := range snippet.Features {
				features[feature] = true
			}
			combined := ""
			for _, source := range snippet.Sources {
				combined += "\n" + source
			}
			for _, word := range snippet.ReservedWords {
				if !containsWord(combined, word) {
					return fmt.Errorf("snippet %q claims reserved word %q but does not contain it", snippet.ID, word)
				}
				words[word] = true
			}
		}
	}
	for _, feature := range RequiredFeatures {
		if !features[feature] {
			return fmt.Errorf("snippet catalog does not cover feature %q", feature)
		}
	}
	for _, word := range RequiredReservedWords {
		if !words[word] {
			return fmt.Errorf("snippet catalog does not cover reserved word %q", word)
		}
	}
	return nil
}

// LineLimitWarnings reports snippets longer than the 20-non-empty-line
// catalog target. The limit is a soft upper bound: longer
// snippets are reported, never rejected.
func LineLimitWarnings(categories []Category) []string {
	warnings := make([]string, 0)
	for _, category := range categories {
		for _, snippet := range category.Snippets {
			lines := 0
			for _, source := range snippet.Sources {
				for _, line := range strings.Split(source, "\n") {
					if strings.TrimSpace(line) != "" {
						lines++
					}
				}
			}
			if lines > 20 {
				warnings = append(warnings, fmt.Sprintf("%s/%s: %d non-empty source lines (target <= 20)", category.ID, snippet.ID, lines))
			}
		}
	}
	return warnings
}

func containsWord(source, word string) bool {
	return slices.Contains(strings.FieldsFunc(source, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_')
	}), word)
}
