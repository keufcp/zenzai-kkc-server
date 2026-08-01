//go:build linux

package main

import "time"

// コンパイル時に決まる値をまとめる．環境変数で変えられる設定は main.go の config を参照．
// 個々のファイルに散らすと，互いの関係が見えなくなるため 1 箇所に置く．
const (
	// maxTextLength は受け付ける読みの最大文字数．超えた要求は 400 で断る．
	// docs/openapi.yaml の text.maxLength と一致させること．
	maxTextLength = 256

	// chunkLimit は 1 回の変換に渡す読みの最大文字数．
	//
	// 変換器は読みと変換候補の両方を 512 トークンのバッファに載せる．
	// 超えると範囲外書き込みで異常終了するため，十分に下回る長さで分割する．
	// 実測では 80 文字前後の分割が最も精度が高い．
	chunkLimit = 80

	// queueLimit は処理待ちにできる要求の数．超えた要求は 503 で断る．
	queueLimit = 8

	// lineBufferSize は変換器の出力を溜めるチャネルの長さ．
	lineBufferSize = 256

	// convertTimeout は変換 1 件の制限時間．超えたら変換器を停止する．
	convertTimeout = 5 * time.Second

	// startupTimeout は変換器の起動からウォームアップ完了までの制限時間．
	// 辞書とモデルのロードを含む．
	startupTimeout = 60 * time.Second

	// stopTimeout は変換器の終了を待つ時間．超えたら kill する．
	stopTimeout = 5 * time.Second

	// HTTP サーバの制限時間．
	readHeaderTimeout = 5 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)
