package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

func TestSnippetsEndpointReturnsValidatedCatalog(t *testing.T) {
	catalog, err := snippets.Load()
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/snippets", nil)
	response := httptest.NewRecorder()
	routes(catalog).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var received []snippets.Category
	if err := json.NewDecoder(response.Body).Decode(&received); err != nil {
		t.Fatal(err)
	}
	if len(received) != len(catalog) {
		t.Fatalf("category count = %d, want %d", len(received), len(catalog))
	}
}

func TestCompileEndpointAcceptsSourceMapAndReturnsGeneratedFiles(t *testing.T) {
	body := []byte(`{"sources":{"app.hex":"answer: Int32 := 42\n"},"entrypoint":"app.hex"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/compile", bytes.NewReader(body))
	response := httptest.NewRecorder()
	routes(nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var result compileResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("exit code = %d, stderr = %v", result.ExitCode, result.Stderr)
	}
	for _, name := range []string{"hexal.h", "modules/app.c", "modules/app.h"} {
		if result.Files[name] == "" {
			t.Errorf("generated file %q is missing or empty", name)
		}
	}
}

func TestCompileEndpointRequiresSourcesAndEntrypoint(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/compile", bytes.NewBufferString(`{"sources":{}}`))
	response := httptest.NewRecorder()
	routes(nil).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

// The workbench is a temporary local debug component: it binds loopback only,
// and main's startup log and its ListenAndServe call share this one constant
// so they can never name different endpoints.
func TestWorkbenchBindsLoopbackOnly(t *testing.T) {
	if workbenchAddress != "127.0.0.1:8080" {
		t.Fatalf("workbenchAddress = %q, want the fixed loopback endpoint 127.0.0.1:8080", workbenchAddress)
	}
	listener, err := net.Listen("tcp", workbenchAddress)
	if err != nil {
		t.Fatalf("failed to bind %s: %v", workbenchAddress, err)
	}
	defer listener.Close()
	host, _, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split listener address: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("listener host = %q, want 127.0.0.1", host)
	}
}
