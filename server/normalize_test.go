//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestNormalizeAcceptsValidInput(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"plain":                        {"あいうえお", "あいうえお"},
		"half-width space":             {"あい うえお", "あいうえお"},
		"full-width space":             {"あい　うえお", "あいうえお"},
		"LF":                           {"あい\nうえお", "あいうえお"},
		"CRLF":                         {"あい\r\nうえお", "あいうえお"},
		"mixed":                        {"あ い　う\nえ\rお", "あいうえお"},
		"keeps punctuation":            {"あい、うえお。", "あい、うえお。"},
		"keeps alphanumerics":          {"あいうえおr2", "あいうえおr2"},
		"exactly at the limit":         {strings.Repeat("あ", maxTextLength), strings.Repeat("あ", maxTextLength)},
		"shortened by removing spaces": {strings.Repeat("あ ", maxTextLength/2), strings.Repeat("あ", maxTextLength/2)},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := normalize(c.in)
			if !ok {
				t.Fatalf("should be accepted: %q", c.in)
			}

			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNormalizeRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"empty":                   "",
		"only half-width spaces":  "   ",
		"only full-width spaces":  "　　　",
		"only newlines":           "\n\r\n",
		"one rune over the limit": strings.Repeat("あ", maxTextLength+1),
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := normalize(in); ok {
				t.Errorf("should be rejected: %q", in)
			}
		})
	}
}
