//go:build linux

package main

import "strings"

// 分割点として優先する文字．文末記号を先に見る．
const (
	sentenceEnd = "。！？"
	clauseEnd   = "、"
)

// 読みを chunkLimit 以下の断片へ分割する．
// 句読点で切ることを優先し，見つからない場合は長さで切る．
func splitReading(reading string) []string {
	runes := []rune(reading)
	if len(runes) <= chunkLimit {
		return []string{reading}
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + chunkLimit
		if end >= len(runes) {
			chunks = append(chunks, string(runes[start:]))
			break
		}
		cut := lastIndexAny(runes[start:end], sentenceEnd)
		if cut < 0 {
			cut = lastIndexAny(runes[start:end], clauseEnd)
		}
		// 句読点が断片の前半にしか無い場合は使わない．細かく切れすぎるため．
		if cut < chunkLimit/2 {
			cut = chunkLimit - 1
		}
		chunks = append(chunks, string(runes[start:start+cut+1]))
		start += cut + 1
	}
	return chunks
}

// runes の中で chars のいずれかが最後に現れる位置を返す．無ければ -1．
func lastIndexAny(runes []rune, chars string) int {
	for i := len(runes) - 1; i >= 0; i-- {
		if strings.ContainsRune(chars, runes[i]) {
			return i
		}
	}
	return -1
}
