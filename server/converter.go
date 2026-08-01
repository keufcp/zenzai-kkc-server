//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// stdbuf は行バッファリングを強制するために挟む．
const stdbufCommand = "stdbuf"

var (
	// anco の出力は ANSI の装飾を含む．
	ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// 候補行は "0. <変換結果>" の形式で出る．
	candidate = regexp.MustCompile(`^0\.\s(.*)$`)

	errUnavailable = errors.New("converter unavailable")
	errNoStdbuf    = errors.New("stdbuf not found")
)

// ancoConverter は anco session を子プロセスとして常駐させ，変換要求を直列に処理する．
type ancoConverter struct {
	anco           string
	model          string
	inferenceLimit int

	// ready はウォームアップ完了後に true になる．
	// ロックを取らずに読めるようにして，変換中でもレスポンスを返せるようにする．
	ready atomic.Bool
	// starting は起動処理中を表す．待たせずにエラーを返すために使う．
	starting atomic.Bool

	// sem は変換を直列化する．context のキャンセルで待機を打ち切れるよう
	// Mutex ではなくチャネルにする．保持している間だけ cmd / stdin / lines を触れる．
	sem chan struct{}

	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string

	waitMu  sync.Mutex
	waiting int
}

func newAncoConverter(anco, model string, inferenceLimit int) *ancoConverter {
	return &ancoConverter{
		anco:           anco,
		model:          model,
		inferenceLimit: inferenceLimit,
		sem:            make(chan struct{}, 1),
	}
}

func (c *ancoConverter) Start(ctx context.Context) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()

	return c.startHeld()
}

func (c *ancoConverter) Stop() {
	c.sem <- struct{}{}
	defer c.release()

	c.stopHeld()
}

// 変換中でもブロックしない．
func (c *ancoConverter) Alive() bool {
	return c.ready.Load()
}

func (c *ancoConverter) Convert(ctx context.Context, text string) (string, error) {
	if err := c.enter(); err != nil {
		return "", err
	}
	defer c.leave()

	if err := c.acquire(ctx); err != nil {
		return "", err
	}
	defer c.release()

	// 前回の変換が失敗して停止したままなら起動し直す．
	// 一度の失敗で恒久的に停止したままにならないようにするため．
	if !c.ready.Load() {
		if err := c.startHeld(); err != nil {
			slog.Error("failed to restart the converter", "error", err)

			return "", fmt.Errorf("%w: the converter is not running", errUnavailable)
		}
	}

	// 変換器が扱える長さに分割して変換し，結果を連結する．
	// 断片どうしは独立しており，左文脈を渡しても精度は改善しない．
	var b strings.Builder

	for _, chunk := range splitReading(text) {
		part, err := c.converse(ctx, chunk, convertTimeout)
		if err != nil {
			// 応答しない変換器を次の要求へ持ち越さない．
			// 次の Convert が起動し直す．
			c.stopHeld()

			return "", err
		}

		b.WriteString(part)
	}

	return b.String(), nil
}

// context がキャンセルされたら待機を打ち切る．
func (c *ancoConverter) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", errUnavailable, ctx.Err())
	}
}

func (c *ancoConverter) release() {
	<-c.sem
}

// sem を取得する前に呼ぶこと．
func (c *ancoConverter) enter() error {
	// 起動中は待たせずに断る．ウォームアップは最大 startupTimeout かかる．
	if c.starting.Load() {
		return fmt.Errorf("%w: the converter is starting", errUnavailable)
	}

	c.waitMu.Lock()
	defer c.waitMu.Unlock()

	if c.waiting >= queueLimit {
		return fmt.Errorf("%w: the request queue is full", errUnavailable)
	}

	c.waiting++

	return nil
}

func (c *ancoConverter) leave() {
	c.waitMu.Lock()
	defer c.waitMu.Unlock()

	c.waiting--
}

// ウォームアップまで済ませる．sem を保持して呼ぶこと．
func (c *ancoConverter) startHeld() error {
	c.starting.Store(true)
	defer c.starting.Store(false)

	if err := c.spawn(); err != nil {
		return err
	}

	// 起動直後は辞書とモデルのロードが走る．最初の実要求を待たせないよう
	// ここで 1 回変換しておく．リクエストの context には影響されない．
	if _, err := c.converse(context.Background(), "ここではきものをぬいでください", startupTimeout); err != nil {
		c.stopHeld()

		return err
	}

	// ウォームアップが終わってから受付可能とする．
	c.ready.Store(true)

	slog.Info("converter started", "pid", c.cmd.Process.Pid)

	return nil
}

// ウォームアップは行わない．sem を保持して呼ぶこと．
func (c *ancoConverter) spawn() error {
	// stdbuf が無いと 1 件ごとに結果を取り出せない．警告ではなく起動を失敗させる．
	if _, err := exec.LookPath(stdbufCommand); err != nil {
		return fmt.Errorf("%w: %w", errNoStdbuf, err)
	}

	args := []string{
		"-oL", c.anco,
		"session",
		"--only_whole_conversion", "-n", "1",
		"--zenz", c.model,
		"--zenz_v3",
		"--config_zenzai_inference_limit", strconv.Itoa(c.inferenceLimit),
	}

	// 子プロセスはリクエストより長く生きる．CommandContext を使うと
	// 1 件のキャンセルで常駐プロセスが落ちる．停止は Stop() が行う．
	cmd := exec.Command(stdbufCommand, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to open stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to open stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the converter: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.lines = make(chan string, lineBufferSize)

	go readLines(stdout, c.lines)

	return nil
}

// sem を保持して呼ぶこと．
func (c *ancoConverter) stopHeld() {
	if c.cmd == nil {
		return
	}

	c.ready.Store(false)

	if c.stdin != nil {
		_, _ = io.WriteString(c.stdin, ":q\n")
		_ = c.stdin.Close()
	}

	// 出力を読み捨てる．readLines がチャネル送信で止まったままだと
	// 子プロセスの書き込みも詰まり，終了しなくなる．
	lines := c.lines

	go func() {
		for line := range lines {
			_ = line
		}
	}()

	done := make(chan struct{})

	go func() {
		_ = c.cmd.Wait()
		close(done)
	}()

	timer := time.NewTimer(stopTimeout)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		_ = c.cmd.Process.Kill()
		<-done
	}

	c.cmd = nil
	c.stdin = nil
	c.lines = nil
}

// 子プロセスへ 1 行送り，応答を読み取る．sem を保持して呼ぶこと．
//
// 失敗した場合，チャネルに読み残しが生じうる．呼び出し側は停止させること．
// 停止せずに次の変換を行うと，遅れて届いた行を応答と取り違える．
func (c *ancoConverter) converse(ctx context.Context, text string, timeout time.Duration) (string, error) {
	if _, err := io.WriteString(c.stdin, text+"\n"); err != nil {
		return "", fmt.Errorf("%w: failed to write to the converter", errUnavailable)
	}

	deadline := time.Now().Add(timeout)
	result := ""

	// 1 件分の出力は "Time: <秒>" で閉じる．
	for {
		line, err := c.readLine(ctx, deadline)
		if err != nil {
			return "", err
		}

		if m := candidate.FindStringSubmatch(line); m != nil {
			result = m[1]

			continue
		}

		if strings.HasPrefix(line, "Time:") {
			break
		}
	}

	if err := c.clearComposition(ctx, deadline); err != nil {
		return "", err
	}

	return result, nil
}

// 次の変換が直前の入力に連結されるのを防ぐ．
func (c *ancoConverter) clearComposition(ctx context.Context, deadline time.Time) error {
	if _, err := io.WriteString(c.stdin, ":c\n"); err != nil {
		return fmt.Errorf("%w: failed to write to the converter", errUnavailable)
	}

	for {
		line, err := c.readLine(ctx, deadline)
		if err != nil {
			return err
		}

		if strings.Contains(line, "composition is stopped") {
			return nil
		}
	}
}

func (c *ancoConverter) readLine(ctx context.Context, deadline time.Time) (string, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return "", fmt.Errorf("%w: the converter did not respond in time", errUnavailable)
	}

	timer := time.NewTimer(remaining)
	defer timer.Stop()

	select {
	case line, ok := <-c.lines:
		if !ok {
			return "", fmt.Errorf("%w: the converter exited", errUnavailable)
		}

		return line, nil
	case <-timer.C:
		return "", fmt.Errorf("%w: the converter did not respond in time", errUnavailable)
	case <-ctx.Done():
		return "", fmt.Errorf("%w: %w", errUnavailable, ctx.Err())
	}
}

// 子プロセスの標準出力を 1 行ずつチャネルへ流す．ANSI の装飾は取り除く．
func readLines(stdout io.Reader, out chan<- string) {
	defer close(out)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		out <- ansi.ReplaceAllString(scanner.Text(), "")
	}

	if err := scanner.Err(); err != nil {
		slog.Error("failed to read from the converter", "error", err)
	}
}
