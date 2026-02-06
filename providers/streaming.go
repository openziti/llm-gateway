package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter provides utilities for writing Server-Sent Events.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSEWriter for the given ResponseWriter.
// Returns nil if the ResponseWriter doesn't support flushing.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	return &SSEWriter{w: w, flusher: flusher}
}

// WriteHeaders sets the appropriate headers for SSE streaming.
func (s *SSEWriter) WriteHeaders() {
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
}

// WriteEvent writes a single SSE event.
func (s *SSEWriter) WriteEvent(data []byte) error {
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteChunk marshals and writes a StreamChunk as an SSE event.
func (s *SSEWriter) WriteChunk(chunk *StreamChunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	return s.WriteEvent(data)
}

// WriteDone writes the final [DONE] event.
func (s *SSEWriter) WriteDone() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteError writes an error as an SSE event.
func (s *SSEWriter) WriteError(apiErr *APIError) error {
	errResp := ErrorResponse{Error: *apiErr}
	data, err := json.Marshal(errResp)
	if err != nil {
		return err
	}
	return s.WriteEvent(data)
}
