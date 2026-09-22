package generator

// Component artifacts: the demand-driven runtime support files under
// generated hexal/. Their source of truth is the embedded C/header
// templates in packages/; Go builds typed render models and selects which
// components a compilation emits. Templates contain presentation only: every
// semantic and selection decision stays in Go.

import (
	"embed"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/template"

	"hexal/compiler/corelib"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
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
// The core-library package's runtime templates join the same set: a core
// library renders through the identical component machinery, just from its
// own embedded directory.
func parseComponentTemplates() map[string]*template.Template {
	parsed := make(map[string]*template.Template)
	parse := func(name, body string) {
		if _, exists := parsed[name]; exists {
			panic("generator: duplicate embedded template " + name)
		}
		instance, err := template.New(name).Option("missingkey=error").Parse(body)
		if err != nil {
			panic("generator: embedded template " + name + " does not parse: " + err.Error())
		}
		parsed[name] = instance
	}
	entries, err := packageTemplates.ReadDir("packages")
	if err != nil {
		panic("generator: cannot read embedded package templates: " + err.Error())
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		body, err := packageTemplates.ReadFile("packages/" + name)
		if err != nil {
			panic("generator: cannot read embedded template " + name + ": " + err.Error())
		}
		parse(name, string(body))
	}
	corelibTemplates, err := corelib.RuntimeTemplates()
	if err != nil {
		panic("generator: cannot read embedded core-library templates: " + err.Error())
	}
	for name, body := range corelibTemplates {
		parse(name, body)
	}
	return parsed
}

// renderComponent executes one component template, or the named sub-template
// when block is set. A parse or execution failure is an internal compiler
// error: generation fails closed and returns no partial artifacts.
func renderComponent(component componentArtifact) (string, error) {
	instance, ok := componentTemplates[component.template]
	if !ok {
		return "", compilerTypes.Diagnostic{Category: compilerTypes.UnknownError, Stage: "generator", Message: fmt.Sprintf("missing embedded component template %s", component.template)}
	}
	var result strings.Builder
	if component.block != "" {
		if err := instance.ExecuteTemplate(&result, component.block, component.model); err != nil {
			return "", compilerTypes.Diagnostic{Category: compilerTypes.UnknownError, Stage: "generator", Message: fmt.Sprintf("component %s block %s render failed: %v", component.key, component.block, err)}
		}
		return result.String(), nil
	}
	if err := instance.Execute(&result, component.model); err != nil {
		return "", compilerTypes.Diagnostic{Category: compilerTypes.UnknownError, Stage: "generator", Message: fmt.Sprintf("component %s render failed: %v", component.key, err)}
	}
	return result.String(), nil
}

// renderInto executes one named sub-template of an embedded template
// directly into dst. A missing template or failed execution is an internal
// compiler error: generation fails closed, mirroring renderComponent.
func renderInto(dst io.Writer, templateName, block string, model any) error {
	instance, ok := componentTemplates[templateName]
	if !ok {
		return compilerTypes.Diagnostic{Category: compilerTypes.UnknownError, Stage: "generator", Message: fmt.Sprintf("missing embedded component template %s", templateName)}
	}
	if err := instance.ExecuteTemplate(dst, block, model); err != nil {
		return compilerTypes.Diagnostic{Category: compilerTypes.UnknownError, Stage: "generator", Message: fmt.Sprintf("template %s block %s render failed: %v", templateName, block, err)}
	}
	return nil
}

// componentTemplateNames returns every embedded template name in
// deterministic order, for tests.
func componentTemplateNames() []string {
	names := make([]string, 0, len(componentTemplates))
	for name := range componentTemplates {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// componentDemand pairs the registry identities a builder owns with the
// builder itself. The builder's Go body is the demand predicate; the
// identities connect whatever it emits to the registry's file, dependency, and
// C-header metadata. corelibComponents is the one builder that can emit two
// components, because program and entropy share a single demand site.
type componentDemand struct {
	ids   []specdata.ComponentID
	build func(*programEmission) ([]componentArtifact, error)
}

// componentDemands lists every demand-driven component with the explicit Go
// builder that decides whether it is emitted. Order does not affect output:
// rendered artifacts land in a keyed map.
func componentDemands(config Config) []componentDemand {
	return []componentDemand{
		{ids: []specdata.ComponentID{specdata.ComponentRuntime}, build: runtimeComponents},
		{ids: []specdata.ComponentID{specdata.ComponentWrap}, build: wrapComponents},
		{ids: []specdata.ComponentID{specdata.ComponentHeap}, build: heapComponents},
		{ids: []specdata.ComponentID{specdata.ComponentSlice}, build: sliceComponents},
		{ids: []specdata.ComponentID{specdata.ComponentString}, build: stringComponents},
		{ids: []specdata.ComponentID{specdata.ComponentError}, build: errorComponents},
		{ids: []specdata.ComponentID{specdata.ComponentSeek}, build: seekComponents},
		{ids: []specdata.ComponentID{specdata.ComponentStash}, build: stashComponents},
		{ids: []specdata.ComponentID{specdata.ComponentPool}, build: poolComponents},
		{ids: []specdata.ComponentID{specdata.ComponentList}, build: listComponents},
		{ids: []specdata.ComponentID{specdata.ComponentDict}, build: dictComponents},
		{ids: []specdata.ComponentID{specdata.ComponentArray}, build: arrayComponents},
		{ids: []specdata.ComponentID{specdata.ComponentNumeric}, build: numericComponents},
		{ids: []specdata.ComponentID{specdata.ComponentPrint}, build: printComponents},
		{ids: []specdata.ComponentID{specdata.ComponentEquality}, build: equalityComponents},
		{ids: []specdata.ComponentID{specdata.ComponentIO}, build: func(merged *programEmission) ([]componentArtifact, error) {
			return ioComponents(merged, config)
		}},
		{ids: []specdata.ComponentID{specdata.ComponentConcurrency}, build: func(merged *programEmission) ([]componentArtifact, error) {
			return concurrencyComponents(merged, config)
		}},
		{ids: []specdata.ComponentID{specdata.ComponentEvent}, build: eventComponents},
		{ids: []specdata.ComponentID{specdata.ComponentTime}, build: timeComponents},
		{ids: []specdata.ComponentID{specdata.ComponentHandle}, build: handleComponents},
		{ids: []specdata.ComponentID{specdata.ComponentFile}, build: fileComponents},
		{ids: []specdata.ComponentID{specdata.ComponentNetwork}, build: networkComponents},
		{ids: []specdata.ComponentID{specdata.ComponentProcess}, build: processComponents},
		{ids: []specdata.ComponentID{specdata.ComponentSignal}, build: signalComponents},
		{ids: []specdata.ComponentID{specdata.ComponentTerminal}, build: func(merged *programEmission) ([]componentArtifact, error) {
			return terminalComponents(merged, config)
		}},
		{ids: []specdata.ComponentID{specdata.ComponentProgram, specdata.ComponentEntropy}, build: func(merged *programEmission) ([]componentArtifact, error) {
			return corelibComponents(merged, config)
		}},
	}
}

// renderComponentArtifacts renders the selected support components of one
// compilation. An optional artifact is omitted when its rendered content is
// empty, so no empty placeholder file is ever emitted. config reaches the
// concurrency runtime core, the one family with build-time settings.
func renderComponentArtifacts(merged *programEmission, config Config) (map[string]string, error) {
	artifacts := make(map[string]string)
	var components []componentArtifact
	for _, demand := range componentDemands(config) {
		familyArtifacts, err := demand.build(merged)
		if err != nil {
			return nil, err
		}
		for _, artifact := range familyArtifacts {
			if err := validateArtifactOwnership(demand.ids, artifact.key); err != nil {
				return nil, err
			}
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

// validateArtifactOwnership refuses an artifact whose file the registry does
// not attribute to the builder that emitted it. It is fail-closed: a mismatch
// means the registry and the builder disagree about what the component owns, a
// compiler-development defect, never a silent extra artifact.
func validateArtifactOwnership(ids []specdata.ComponentID, key string) error {
	owner, ok := specdata.FileOwner(key)
	if !ok {
		return compilerTypes.Diagnostic{Category: compilerTypes.UnknownError, Stage: "generator", Message: fmt.Sprintf("generated artifact %s is owned by no registered component", key)}
	}
	for _, id := range ids {
		if id == owner {
			return nil
		}
	}
	return compilerTypes.Diagnostic{Category: compilerTypes.UnknownError, Stage: "generator", Message: fmt.Sprintf("component %s emitted artifact %s owned by another component", owner, key)}
}
