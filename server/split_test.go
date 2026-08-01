//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestSplitReadingKeepsShortInputIntact(t *testing.T) {
	in := "わたしのともだちはりょうりがじょうずです"
	got := splitReading(in)
	if len(got) != 1 || got[0] != in {
		t.Fatalf("should not be split: %q", got)
	}
}

func TestSplitReadingRespectsLimit(t *testing.T) {
	cases := []string{
		strings.Repeat("あ", 81),
		strings.Repeat("あ", 256),
		strings.Repeat("あいうえお。", 60),
		strings.Repeat("あいうえお、", 60),
	}
	for _, in := range cases {
		for _, chunk := range splitReading(in) {
			if n := len([]rune(chunk)); n > chunkLimit {
				t.Errorf("chunk exceeds the limit: %d runes (input %d runes)", n, len([]rune(in)))
			}
		}
	}
}

func TestSplitReadingPreservesContent(t *testing.T) {
	cases := []string{
		strings.Repeat("あ", 256),
		"きょうはいいてんきですね。そとにでよう。" + strings.Repeat("あいうえお", 40),
		strings.Repeat("あいうえお、", 60),
	}
	for _, in := range cases {
		if got := strings.Join(splitReading(in), ""); got != in {
			t.Errorf("joining the chunks does not restore the input\n in:  %q\n got: %q", in, got)
		}
	}
}

func TestSplitReadingBreaksAtPunctuation(t *testing.T) {
	// 上限の直前に句点がある場合，そこで切る．
	in := strings.Repeat("あ", 70) + "。" + strings.Repeat("い", 70)
	got := splitReading(in)
	if len(got) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(got))
	}
	if !strings.HasSuffix(got[0], "。") {
		t.Errorf("chunk does not end with a full stop: %q", got[0])
	}
}

func TestSplitReadingHandlesMultibyteBoundary(t *testing.T) {
	// 絵文字などのサロゲートを含む文字でも壊れない．
	in := strings.Repeat("😀", 200)
	got := splitReading(in)
	if strings.Join(got, "") != in {
		t.Error("joining the chunks does not restore the input")
	}
	for _, chunk := range got {
		if n := len([]rune(chunk)); n > chunkLimit {
			t.Errorf("chunk exceeds the limit: %d runes", n)
		}
	}
}
