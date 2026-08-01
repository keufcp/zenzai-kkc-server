//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeConverter は Converter を差し替えて，変換器を起動せずにハンドラを試す．
type fakeConverter struct {
	result string
	err    error
	alive  bool
	got    string
}

func (f *fakeConverter) Convert(_ context.Context, text string) (string, error) {
	f.got = text

	return f.result, f.err
}

func (f *fakeConverter) Alive() bool { return f.alive }

func do(t *testing.T, conv Converter, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	newServer(conv).routes().ServeHTTP(rec, httptest.NewRequest(method, target, nil))

	return rec
}

func convertURL(text string) string {
	return "/v1/convert?" + url.Values{"text": {text}}.Encode()
}

func TestConvertReturnsResult(t *testing.T) {
	conv := &fakeConverter{result: "私の友達", alive: true}

	rec := do(t, conv, http.MethodGet, convertURL("わたしのともだち"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("got content type %q", ct)
	}

	var body struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if body.Result != "私の友達" {
		t.Errorf("got result %q", body.Result)
	}
}

func TestConvertRemovesSpacesAndNewlines(t *testing.T) {
	conv := &fakeConverter{result: "結果", alive: true}

	do(t, conv, http.MethodGet, convertURL("あい うえ　お\nか\rき"))

	if conv.got != "あいうえおかき" {
		t.Errorf("converter received %q", conv.got)
	}
}

func TestConvertRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"no text":        "/v1/convert",
		"empty text":     convertURL(""),
		"only spaces":    convertURL("   "),
		"over the limit": convertURL(strings.Repeat("あ", maxTextLength+1)),
	}

	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(t, &fakeConverter{alive: true}, http.MethodGet, target)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("got status %d", rec.Code)
			}

			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
				t.Errorf("got content type %q", ct)
			}
		})
	}
}

func TestConvertAcceptsExactLimit(t *testing.T) {
	conv := &fakeConverter{result: "結果", alive: true}

	rec := do(t, conv, http.MethodGet, convertURL(strings.Repeat("あ", maxTextLength)))
	if rec.Code != http.StatusOK {
		t.Errorf("got status %d", rec.Code)
	}
}

func TestConvertMapsErrorsToStatus(t *testing.T) {
	cases := []struct {
		name string
		conv *fakeConverter
		want int
	}{
		{"converter unavailable", &fakeConverter{err: errUnavailable, alive: true}, http.StatusServiceUnavailable},
		{"other failure", &fakeConverter{err: errors.New("boom"), alive: true}, http.StatusInternalServerError},
		{"empty result", &fakeConverter{result: "", alive: true}, http.StatusInternalServerError},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, c.conv, http.MethodGet, convertURL("あ"))
			if rec.Code != c.want {
				t.Errorf("got status %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestHealthReflectsConverterState(t *testing.T) {
	cases := []struct {
		alive  bool
		want   int
		status string
	}{
		{true, http.StatusOK, "ok"},
		{false, http.StatusServiceUnavailable, "unavailable"},
	}

	for _, c := range cases {
		rec := do(t, &fakeConverter{alive: c.alive}, http.MethodGet, "/v1/health")
		if rec.Code != c.want {
			t.Errorf("got status %d, want %d", rec.Code, c.want)
		}

		var body struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if body.Status != c.status {
			t.Errorf("got status %q, want %q", body.Status, c.status)
		}
	}
}

func TestRejectsNonGet(t *testing.T) {
	for _, path := range []string{"/v1/convert", "/v1/health"} {
		rec := do(t, &fakeConverter{alive: true}, http.MethodPost, path)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: got status %d", path, rec.Code)
		}

		if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
			t.Errorf("%s: got Allow %q", path, allow)
		}
	}
}

func TestUnknownPathReturnsNotFound(t *testing.T) {
	rec := do(t, &fakeConverter{alive: true}, http.MethodGet, "/v1/nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("got status %d", rec.Code)
	}
}
