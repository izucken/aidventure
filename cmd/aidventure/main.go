// Command aidventure runs the play server: it talks to the models and serves the browser UI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aidventure/internal/attribute"
	"aidventure/internal/config"
	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/httpserver"
	"aidventure/internal/llm"
	"aidventure/internal/model"
	"aidventure/internal/play"
	"aidventure/internal/scene"
	"aidventure/internal/store"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
	"aidventure/internal/ui"
)

func main() {
	if err := run(); err != nil {
		slog.Error("aidventure exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	db, err := store.Open(ctx, filepath.Join(cfg.DataDir, "aidventure.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	front, err := frontend(cfg)
	if err != nil {
		return err
	}

	deps := httpserver.Deps{
		Games:      game.NewStore(db),
		Threads:    thread.NewStore(db),
		Subjects:   subject.NewStore(db),
		Attributes: attribute.NewStore(db),
		Scenes:     scene.NewStore(db),
		Entries:    entry.NewStore(db),
		Models:     model.NewStore(db),
		DB:         db,
	}
	deps.Roller = &play.Roller{
		DB: db, Models: deps.Models, Threads: deps.Threads, Subjects: deps.Subjects, Scenes: deps.Scenes, Entries: deps.Entries,
		Engine: llm.New,
		Log:    log,
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpserver.New(log, front, deps),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "ui", uiSource(cfg), "data", cfg.DataDir)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// frontend picks where UI requests go: the Vite dev server while developing,
// the bundle compiled into the binary otherwise.
func frontend(cfg config.Config) (http.Handler, error) {
	if cfg.UIDevServer != "" {
		return httpserver.DevProxy(cfg.UIDevServer)
	}
	bundle, err := ui.Bundle()
	if err != nil {
		return nil, err
	}
	return httpserver.SPA(bundle)
}

func uiSource(cfg config.Config) string {
	if cfg.UIDevServer != "" {
		return "proxy " + cfg.UIDevServer
	}
	return "embedded"
}
