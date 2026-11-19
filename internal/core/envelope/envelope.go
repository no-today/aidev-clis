// Package envelope writes the minimal AI-first output contract:
// success -> {"data": <payload>} (+ "warnings" only when present),
// error   -> {"error":{"code","message"}},
// streaming -> NDJSON {"type":"record"|"end"|"error", ...}.
//
// The *Ctx variants additionally drain any opt-in diagnostics collected on the
// context (via package diag) into a top-level "diagnostics" field — present
// only when the user passed -v/-vv and lines were recorded. With no collector
// the output is byte-for-byte identical to the plain writers.
package envelope

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/no-today/aidev-clis/internal/core/diag"
)

// newEncoder builds the JSON encoder used everywhere. HTML escaping is OFF:
// this is a CLI, not a web page, and log lines / SQL routinely contain
// `<`, `>`, `&` — escaping them to < etc. would mangle the payload an AI
// reads back.
func newEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc
}

func encode(w io.Writer, v any, pretty bool) error {
	enc := newEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

// WriteData emits {"data":payload} plus "warnings" when len(warnings)>0.
func WriteData(w io.Writer, payload any, warnings []string, pretty bool) error {
	out := map[string]any{"data": payload}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return encode(w, out, pretty)
}

// WriteRawError writes a single un-enveloped error line: "ERROR <code>: <message>".
// Used by --output raw paths across the CLIs so the plain-error format lives in
// one place (the JSON {error} envelope is WriteError).
func WriteRawError(w io.Writer, code, msg string) error {
	_, err := fmt.Fprintf(w, "ERROR %s: %s\n", code, msg)
	return err
}

// WriteError emits {"error":{"code","message"}}.
func WriteError(w io.Writer, code, message string, pretty bool) error {
	return encode(w, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	}, pretty)
}

// WriteDataCtx is WriteData plus the ctx's diagnostics (omitted when empty).
func WriteDataCtx(ctx context.Context, w io.Writer, payload any, warnings []string, pretty bool) error {
	out := map[string]any{"data": payload}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	if d := diag.From(ctx).Lines(); len(d) > 0 {
		out["diagnostics"] = d
	}
	return encode(w, out, pretty)
}

// WriteErrorCtx is WriteError plus the ctx's diagnostics (omitted when empty).
func WriteErrorCtx(ctx context.Context, w io.Writer, code, message string, pretty bool) error {
	out := map[string]any{
		"error": map[string]string{"code": code, "message": message},
	}
	if d := diag.From(ctx).Lines(); len(d) > 0 {
		out["diagnostics"] = d
	}
	return encode(w, out, pretty)
}

// Stream writes NDJSON records, one JSON object per line, flushing each.
type Stream struct {
	mu sync.Mutex
	w  io.Writer
}

func NewStream(w io.Writer) *Stream { return &Stream{w: w} }

func (s *Stream) line(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return newEncoder(s.w).Encode(v) // Encoder.Encode appends '\n'
}

// Stream line shapes. Typed structs (not maps) so the JSON discriminator
// "type" is always the first field — matches docs/OUTPUT-CONTRACT.md and the
// discriminator-first NDJSON convention. (map[string]any would alphabetize.)
type recordLine struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
type endLine struct {
	Type        string   `json:"type"`
	Count       int      `json:"count"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}
type errLine struct {
	Type        string     `json:"type"`
	Error       errPayload `json:"error"`
	Diagnostics []string   `json:"diagnostics,omitempty"`
}
type errPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Stream) Record(data any) error { return s.line(recordLine{Type: "record", Data: data}) }
func (s *Stream) End(count int) error   { return s.line(endLine{Type: "end", Count: count}) }
func (s *Stream) Err(code, msg string) error {
	return s.line(errLine{Type: "error", Error: errPayload{Code: code, Message: msg}})
}

// EndCtx is End plus the ctx's diagnostics on the terminal line (omitted when
// empty) — the streaming analogue of WriteDataCtx.
func (s *Stream) EndCtx(ctx context.Context, count int) error {
	return s.line(endLine{Type: "end", Count: count, Diagnostics: diag.From(ctx).Lines()})
}

// ErrCtx is Err plus the ctx's diagnostics on the terminal line (omitted when empty).
func (s *Stream) ErrCtx(ctx context.Context, code, msg string) error {
	return s.line(errLine{Type: "error", Error: errPayload{Code: code, Message: msg}, Diagnostics: diag.From(ctx).Lines()})
}
