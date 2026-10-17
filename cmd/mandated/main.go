// Command mandated runs the Mandate API, webhook receiver and background
// reconciliation in one process (modular monolith, ADR-0001).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/api"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/config"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/mcpserver"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("mandated exited", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rt, err := config.Load(ctx, log)
	if err != nil {
		return err
	}
	defer rt.Store.Close()
	if err := rt.Store.Migrate(ctx); err != nil {
		return err
	}
	log.Info("database migrated")

	srv := api.New(rt.API, rt.Kernel, rt.Store, rt.PayPal, rt.Hub, log)
	srv.MountAgents(rt.Agents)
	srv.Mount("/mcp/", mcpserver.Handler(mcpserver.Deps{Kernel: rt.Kernel, Store: rt.Store, Refunder: rt.PayPal, AgentID: "agent:procure-01"}))
	go rt.Kernel.Sweep(ctx, kernel.SweepConfig{Interval: 30 * time.Second, SettleAfter: 60 * time.Second, SyncAfter: 30 * time.Second}, log)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "port", port, "paypal_env", rt.PayPal.Env(), "public_base_url", rt.API.PublicBaseURL)
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdown)
}
