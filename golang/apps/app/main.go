// Command app runs this project's three roles in one process: the harness that
// holds the clock, the read-side API that serves the demo page, and the MCP
// server the agent uses to record its intent and read its own state.
//
// ROLES chooses which of them run. Everything else is read from the environment
// by internal/config.
package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/api"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/config"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/harness"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/mcpserver"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/store"
)

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = log.Sync() }()

	if err := run(log); err != nil {
		log.Fatal("stopped", zap.Error(err))
	}
}

func run(log *zap.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	state := store.NewMemory()
	group, ctx := errgroup.WithContext(ctx)

	if cfg.Has(config.RoleAPI) {
		handler, err := api.Handler(state, cfg.WebDir, log.Named("api"))
		if err != nil {
			return err
		}
		group.Go(func() error { return serve(ctx, cfg.Addr, handler, log.Named("api")) })
	}

	if cfg.Has(config.RoleMCP) {
		handler := mcpserver.Handler(state, time.Now)
		group.Go(func() error { return serve(ctx, cfg.MCPAddr, handler, log.Named("mcp")) })
	}

	if cfg.Has(config.RoleHarness) {
		group.Go(func() error { return harness.Run(ctx, cfg.DeclarationPath, log.Named("harness")) })
	}

	log.Info("started", zap.Any("roles", cfg.Roles))

	return group.Wait()
}

// serve runs one HTTP listener until ctx ends, then gives in-flight requests the
// shutdown grace the caller allows. The grace is short on purpose: every request
// this process serves is a read.
func serve(ctx context.Context, addr string, handler http.Handler, log *zap.Logger) error {
	if addr == "" {
		return errors.New("no address configured for this role")
	}
	server := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Error("shutdown", zap.Error(err))
		}
	}()

	log.Info("listening", zap.String("addr", addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

const shutdownGrace = 5 * time.Second
