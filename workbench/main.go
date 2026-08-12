package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"hexal/compiler"
)

//go:embed index.html
var indexHTML []byte

type compileRequest struct {
	Source string `json:"source"`
}

type compileResponse struct {
	MainC    string        `json:"mainC"`
	MainH    string        `json:"mainH"`
	Stderr   []string      `json:"stderr"`
	ExitCode int           `json:"exitCode"`
	Stats    statsResponse `json:"stats"`
}

type statsResponse struct {
	TokenCount      int           `json:"tokenCount"`
	SourceLines     int           `json:"sourceLines"`
	PixelSubtotalMs float64       `json:"pixelSubtotalMs"`
	TotalMs         float64       `json:"totalMs"`
	Phases          []phaseStatus `json:"phases"`
}

type phaseStatus struct {
	Name         string  `json:"name"`
	Milliseconds float64 `json:"milliseconds"`
	Detail       string  `json:"detail,omitempty"`
}

func main() {
	address := flag.String("addr", ":8080", "workbench listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/api/compile", compileHandler)

	log.Printf("Hexal workbench listening on http://localhost%s", *address)
	if err := http.ListenAndServe(*address, mux); err != nil {
		log.Fatal(err)
	}
}

func serveIndex(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = response.Write(indexHTML)
}

func compileHandler(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input compileRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, compileResponse{
			Stderr:   []string{"workbench: invalid request: " + err.Error()},
			ExitCode: compiler.ExitFailure,
		})
		return
	}

	writeJSON(response, http.StatusOK, toResponse(compiler.Compile(input.Source)))
}

func toResponse(result compiler.CompilationResult) compileResponse {
	return compileResponse{
		MainC:    result.MainC,
		MainH:    result.MainH,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		Stats: statsResponse{
			TokenCount:      result.Stats.TokenCount,
			SourceLines:     result.Stats.SourceLines,
			PixelSubtotalMs: milliseconds(result.Stats.PixelSubtotal),
			TotalMs:         milliseconds(result.Stats.TotalDuration),
			Phases: []phaseStatus{
				phase("Lexer", result.Stats.LexDuration, formatRate(result.Stats.TokenCount, result.Stats.LexDuration, "tokens/sec")),
				phase("Parser", result.Stats.ParseDuration, formatRate(result.Stats.SourceLines, result.Stats.ParseDuration, "LOC/sec")),
				phase("Checker", result.Stats.CheckDuration, ""),
				phase("Generator", result.Stats.GenerateDuration, ""),
			},
		},
	}
}

func phase(name string, duration time.Duration, detail string) phaseStatus {
	return phaseStatus{
		Name:         name,
		Milliseconds: milliseconds(duration),
		Detail:       detail,
	}
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func formatRate(count int, duration time.Duration, unit string) string {
	if count == 0 || duration <= 0 {
		return "n/a"
	}
	perSecond := float64(count) * float64(time.Second) / float64(duration)
	if perSecond >= 1_000_000 {
		return fmt.Sprintf("%.2f M %s", perSecond/1_000_000, unit)
	}
	if perSecond >= 1_000 {
		return fmt.Sprintf("%.0f k %s", perSecond/1_000, unit)
	}
	return fmt.Sprintf("%.0f %s", perSecond, unit)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
