//go:build linux

// zenzai-kkc-server はかな漢字変換の HTTP API を提供する．
// API の仕様は docs/openapi.yaml を参照．
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

const (
	minPort = 1
	maxPort = 65535
)

var (
	errPortRequired = errors.New("ZKKC_PORT is required and has no default")
	errPortInvalid  = errors.New("ZKKC_PORT is invalid")
	errLimitInvalid = errors.New("ZKKC_INFERENCE_LIMIT is invalid")
	errNotFound     = errors.New("not found")
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := run(); err != nil {
		slog.Error("exiting", "error", err)
		os.Exit(1)
	}
}

type config struct {
	bind           string
	port           int
	anco           string
	model          string
	inferenceLimit int
}

func loadConfig() (config, error) {
	cfg := config{
		bind:           envOr("ZKKC_BIND", "127.0.0.1"),
		anco:           envOr("ZKKC_ANCO", "/opt/zenzai-kkc/anco"),
		model:          envOr("ZKKC_MODEL", "/opt/zenzai-kkc/models/zenz-v3.2-xsmall.gguf"),
		port:           0,
		inferenceLimit: 0,
	}

	rawPort := os.Getenv("ZKKC_PORT")
	if rawPort == "" {
		return cfg, errPortRequired
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil || port < minPort || port > maxPort {
		return cfg, fmt.Errorf("%w: %s", errPortInvalid, rawPort)
	}

	cfg.port = port

	rawLimit := envOr("ZKKC_INFERENCE_LIMIT", "1")

	limit, err := strconv.Atoi(rawLimit)
	if err != nil || limit < 1 {
		return cfg, fmt.Errorf("%w: %s", errLimitInvalid, rawLimit)
	}

	cfg.inferenceLimit = limit

	for _, path := range []string{cfg.anco, cfg.model} {
		if _, err := os.Stat(path); err != nil {
			return cfg, fmt.Errorf("%w: %s", errNotFound, path)
		}
	}

	return cfg, nil
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	warnEphemeral(cfg.port)

	// SIGINT / SIGTERM で停止する．子プロセスを孤立させないため．
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conv := newAncoConverter(cfg.anco, cfg.model, cfg.inferenceLimit)
	if err := conv.Start(ctx); err != nil {
		return fmt.Errorf("failed to start the converter: %w", err)
	}
	defer conv.Stop()

	addr := net.JoinHostPort(cfg.bind, strconv.Itoa(cfg.port))

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           newServer(conv).routes(),
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	slog.Info("listening", "addr", addr, "model", cfg.model, "inference_limit", cfg.inferenceLimit)

	return serve(ctx, httpServer)
}

// ctx が終了したらグレースフルシャットダウンする．
func serve(ctx context.Context, httpServer *http.Server) error {
	errCh := make(chan error, 1)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("failed to serve: %w", err)

			return
		}

		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")

		// ctx は既にキャンセル済みなので，値だけ引き継いでキャンセルを外す．
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shut down: %w", err)
		}

		return nil
	}
}

// 指定されたポートが OS の外向き接続用の範囲に入っている場合に警告する．
// この範囲で待ち受けると，その番号が使用中のときに bind が失敗する．
func warnEphemeral(port int) {
	raw, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		return
	}

	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return
	}

	low, errLow := strconv.Atoi(fields[0])

	high, errHigh := strconv.Atoi(fields[1])
	if errLow != nil || errHigh != nil {
		return
	}

	if port >= low && port <= high {
		slog.Warn("the port lies within the ephemeral port range; bind may fail intermittently",
			"port", port, "range_low", low, "range_high", high)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}
