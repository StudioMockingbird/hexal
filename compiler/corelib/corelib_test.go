package corelib

import (
	"testing"

	"hexal/compiler/specdata"
)

// Every type a module exports must resolve through the identifier adapter. A
// record naming an identifier the adapter does not know would silently drop
// that type from import resolution, the failure this guards against.
func TestEveryTypeExportResolves(t *testing.T) {
	for _, module := range specdata.CoreModules() {
		for _, export := range module.Types {
			if _, ok := resolveCoreType(export.TypeID); !ok {
				t.Errorf("%s exports %s with identifier %q, which the adapter does not resolve", module.ID, export.Name, export.TypeID)
			}
		}
	}
}
