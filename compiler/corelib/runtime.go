package corelib

import "embed"

// runtimeTemplates embeds every core-library runtime template. The generator
// reads them by name and merges them into its own template set; corelib never
// parses or renders them itself.
//
//go:embed runtime/*.c runtime/*.h
var runtimeTemplates embed.FS

// RuntimeTemplates returns every embedded runtime template's file name and
// source text, in the runtime/ directory.
func RuntimeTemplates() (map[string]string, error) {
	entries, err := runtimeTemplates.ReadDir("runtime")
	if err != nil {
		return nil, err
	}
	templates := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := runtimeTemplates.ReadFile("runtime/" + entry.Name())
		if err != nil {
			return nil, err
		}
		templates[entry.Name()] = string(body)
	}
	return templates, nil
}
