//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain はテストバイナリを anco の代役としても使えるようにする．
//
// ancoConverter は "<anco> session ..." の形で子プロセスを起動するので，
// 引数に "session" があれば模擬の変換器として振る舞う．
// 外部のスクリプトを置かずに済み，実行権の設定も要らない．
func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		if arg == "session" {
			runFakeAnco()
			os.Exit(0)
		}
	}

	os.Exit(m.Run())
}

// 模擬の変換器の振る舞い．--zenz の値で切り替える．
const (
	modeNormal = "normal" // 正常に応答する
	modeSilent = "silent" // 何も応答しない
	modeFlood  = "flood"  // 応答の前に大量の行を吐く
)

// runFakeAnco は anco session の入出力だけを模す．
func runFakeAnco() {
	mode := modeNormal

	for i, arg := range os.Args {
		if arg == "--zenz" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}

	fmt.Println("== banner ==")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch line := scanner.Text(); line {
		case ":q":
			return
		case ":c":
			fmt.Println("composition is stopped")
		default:
			respond(mode, line)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fake anco: failed to read stdin: %v\n", err)
		os.Exit(1)
	}
}

func respond(mode, line string) {
	switch mode {
	case modeSilent:
		return
	case modeFlood:
		// lineBufferSize を超える行を吐き，チャネルを詰まらせる．
		for i := range lineBufferSize * 3 {
			fmt.Printf("noise %d\n", i)
		}
	}

	fmt.Printf("0. 変換[%s]\n", line)
	fmt.Println("Time: 0.01")
}

// newTestConverter は模擬の変換器を組み立てる．
// mode は --zenz の値として子プロセスへ渡る．
func newTestConverter(t *testing.T, mode string) *ancoConverter {
	t.Helper()

	if _, err := exec.LookPath(stdbufCommand); err != nil {
		t.Skipf("%s is not available", stdbufCommand)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to locate the test binary: %v", err)
	}

	return newAncoConverter(self, mode, 1)
}

func TestConverterStartsAndConverts(t *testing.T) {
	c := newTestConverter(t, modeNormal)

	ctx := t.Context()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer c.Stop()

	if !c.Alive() {
		t.Error("Alive is false after start")
	}

	got, err := c.Convert(ctx, "あいうえお")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if got != "変換[あいうえお]" {
		t.Errorf("got %q", got)
	}
}

func TestConverterSplitsLongInput(t *testing.T) {
	c := newTestConverter(t, modeNormal)

	ctx := t.Context()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer c.Stop()

	// chunkLimit を超えるので複数回に分かれ，結果が連結される．
	in := strings.Repeat("あ", chunkLimit+10)

	got, err := c.Convert(ctx, in)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if want := len(splitReading(in)); strings.Count(got, "変換[") != want {
		t.Errorf("want %d chunks, got %q", want, got)
	}
}

func TestConverterRestartsAfterFailure(t *testing.T) {
	c := newTestConverter(t, modeNormal)

	ctx := t.Context()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer c.Stop()

	// 停止させてから変換を要求すると，自動で起動し直して成功する．
	c.sem <- struct{}{}
	c.stopHeld()
	c.release()

	if c.Alive() {
		t.Fatal("Alive is true after stop")
	}

	got, err := c.Convert(ctx, "あ")
	if err != nil {
		t.Fatalf("should convert after restart: %v", err)
	}

	if got != "変換[あ]" {
		t.Errorf("got %q", got)
	}

	if !c.Alive() {
		t.Error("Alive is false after restart")
	}
}

func TestConverterStopsWhenOutputFloods(t *testing.T) {
	// 応答の前に lineBufferSize を超える行を吐く．
	// readLines がチャネル送信で止まっても Stop が返ることを確かめる．
	c := newTestConverter(t, modeFlood)

	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	done := make(chan struct{})

	go func() {
		c.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(stopTimeout + 5*time.Second):
		t.Fatal("Stop did not return")
	}
}
