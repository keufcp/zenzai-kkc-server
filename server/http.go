//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 変換器が利用できない場合は errUnavailable を包んだエラーを返す．
type Converter interface {
	Convert(ctx context.Context, text string) (string, error)
	Alive() bool
}

type server struct {
	conv Converter
}

func newServer(conv Converter) *server {
	return &server{conv: conv}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/convert", s.handleConvert)
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/", handleNotFound)

	return mux
}

func (s *server) handleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)

		return
	}

	normalized, ok := normalize(r.URL.Query().Get("text"))
	if !ok {
		problem(w, http.StatusBadRequest,
			fmt.Sprintf("text must be between 1 and %d characters.", maxTextLength))

		return
	}

	result, err := s.conv.Convert(r.Context(), normalized)
	if err != nil {
		if errors.Is(err, errUnavailable) {
			slog.Warn("conversion unavailable", "error", err)
			problem(w, http.StatusServiceUnavailable, "The converter is not responding.")

			return
		}

		slog.Error("conversion failed", "error", err)
		problem(w, http.StatusInternalServerError, "The conversion failed.")

		return
	}

	if result == "" {
		slog.Error("conversion produced an empty result", "chars", utf8.RuneCountInString(normalized))
		problem(w, http.StatusInternalServerError, "The conversion produced an empty result.")

		return
	}

	writeJSON(w, http.StatusOK, "application/json", map[string]string{"result": result})
}

// 空白と改行を除去する．ローマ字入力を経由した読みには単語ごとに空白が入るが，
// これを残すと変換器が空白ごとに文脈を切ってしまい，前後の語を考慮できなくなる．
func normalize(text string) (string, bool) {
	if text == "" || utf8.RuneCountInString(text) > maxTextLength {
		return "", false
	}

	normalized := strings.NewReplacer(" ", "", "　", "", "\n", "", "\r", "").Replace(text)
	if normalized == "" {
		return "", false
	}

	return normalized, true
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)

		return
	}

	if s.conv.Alive() {
		writeJSON(w, http.StatusOK, "application/json", map[string]string{"status": "ok"})

		return
	}

	writeJSON(w, http.StatusServiceUnavailable, "application/json",
		map[string]string{"status": "unavailable"})
}

func handleNotFound(w http.ResponseWriter, _ *http.Request) {
	problem(w, http.StatusNotFound, "No such path.")
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet)
	problem(w, http.StatusMethodNotAllowed, "Only GET is allowed on this path.")
}

func problem(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, "application/problem+json", map[string]any{
		"type":   "about:blank",
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}

func writeJSON(w http.ResponseWriter, status int, contentType string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to encode the response", "error", err)
		http.Error(w, "", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)

	if _, err := w.Write(body); err != nil {
		slog.Warn("failed to write the response", "error", err)
	}
}
