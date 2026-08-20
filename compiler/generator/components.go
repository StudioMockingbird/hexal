package generator

// Component artifacts: the demand-driven runtime support files under
// generated hexal/. Their source of truth is the embedded C/header
// templates in packages/; Go builds typed render models and selects which
// components a compilation emits. Templates contain presentation only: every
// semantic and selection decision stays in Go.

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed packages/*.h packages/*.c
var packageTemplates embed.FS

// componentTemplates is the embedded template set parsed once per process.
// Parsing is fail-closed: an asset that cannot parse is a build-time-program
// invariant violation, not a runtime condition.
var componentTemplates = parseComponentTemplates()

// componentArtifact is one generated support file: its logical key under
// generated hexal/, the repository template that owns its text, and the typed
// render model for this compilation. block names the {{define}} sub-template
// when only a fragment of the artifact renders (module headers re-emit
// module-owned collection specializations without the artifact's guard and
// include shell).
type componentArtifact struct {
	key      string
	template string
	block    string
	model    any
}

// parseComponentTemplates parses every embedded package template exactly once.
func parseComponentTemplates() map[string]*template.Template {
	entries, err := packageTemplates.ReadDir("packages")
	if err != nil {
		panic("generator: cannot read embedded package templates: " + err.Error())
	}
	parsed := make(map[string]*template.Template, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		body, err := packageTemplates.ReadFile("packages/" + name)
		if err != nil {
			panic("generator: cannot read embedded template " + name + ": " + err.Error())
		}
		instance, err := template.New(name).Option("missingkey=error").Parse(string(body))
		if err != nil {
			panic("generator: embedded template " + name + " does not parse: " + err.Error())
		}
		parsed[name] = instance
	}
	return parsed
}

// renderComponent executes one component template, or the named sub-template
// when block is set. A parse or execution failure is an internal compiler
// error: generation fails closed and returns no partial artifacts.
func renderComponent(component componentArtifact) (string, error) {
	instance, ok := componentTemplates[component.template]
	if !ok {
		return "", fmt.Errorf("generator: missing embedded component template %s", component.template)
	}
	var result strings.Builder
	if component.block != "" {
		if err := instance.ExecuteTemplate(&result, component.block, component.model); err != nil {
			return "", fmt.Errorf("generator: component %s block %s render failed: %w", component.key, component.block, err)
		}
		return result.String(), nil
	}
	if err := instance.Execute(&result, component.model); err != nil {
		return "", fmt.Errorf("generator: component %s render failed: %w", component.key, err)
	}
	return result.String(), nil
}

// componentTemplateNames returns the repository template names of the
// embedded package set in deterministic order, for tests.
func componentTemplateNames() []string {
	entries, err := packageTemplates.ReadDir("packages")
	if err != nil {
		panic("generator: cannot read embedded package templates: " + err.Error())
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// renderComponentArtifacts renders the selected support components of one
// compilation. An optional artifact is omitted when its rendered content is
// empty, so no empty placeholder file is ever emitted. config reaches the
// concurrency runtime core, the one family with build-time settings.
func renderComponentArtifacts(merged *programEmission, config Config) (map[string]string, error) {
	artifacts := make(map[string]string)
	var components []componentArtifact
	if merged.requirements != nil && merged.requirements.trap {
		components = append(components, componentArtifact{
			key:      "hexal/runtime.c",
			template: "runtime.c",
			model:    struct{}{},
		})
	}
	// Each migrated family contributes its artifacts through its own
	// component builder; families still owned by hexal.h contribute none.
	families := []func(*programEmission) ([]componentArtifact, error){
		wrapComponents,
		heapComponents,
		viewComponents,
		stringComponents,
		errorComponents,
		listComponents,
		dictComponents,
		arrayComponents,
		func(merged *programEmission) ([]componentArtifact, error) {
			return concurrencyComponents(merged, config)
		},
	}
	for _, family := range families {
		familyArtifacts, err := family(merged)
		if err != nil {
			return nil, err
		}
		components = append(components, familyArtifacts...)
	}
	for _, component := range components {
		content, err := renderComponent(component)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		artifacts[component.key] = content
	}
	return artifacts, nil
}
