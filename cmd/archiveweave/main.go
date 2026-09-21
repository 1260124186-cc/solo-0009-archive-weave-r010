package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/solo-0009-archive-weave/internal/catalog"
	"example.com/solo-0009-archive-weave/internal/config"
	"example.com/solo-0009-archive-weave/internal/httpapi"
	"example.com/solo-0009-archive-weave/internal/observability"
	"example.com/solo-0009-archive-weave/internal/storage"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	logger := observability.NewLogger()
	metrics := &observability.Metrics{}
	repository := storage.NewJSONStore(cfg.DataPath)
	auditRepository := storage.NewJSONAuditStore(cfg.AuditPath)
	snapshotRepository := storage.NewJSONSnapshotStore(cfg.SnapshotPath)
	comparisonRepository := storage.NewJSONComparisonStore(cfg.ComparisonPath)
	service := catalog.NewService(repository, auditRepository).WithVersionSnapshots(snapshotRepository)
	comparisonService := catalog.NewComparisonService(
		comparisonRepository, repository, snapshotRepository, cfg.ComparisonWorkers,
	).WithHooks(metrics.Write, func(succeeded bool) {
		metrics.ComparisonItem(succeeded)
	})
	handler := httpapi.NewServer(service, comparisonService, logger, metrics, cfg.ReadOnly).Handler()
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Read-only mode must not recover or advance jobs; stored results remain
	// readable through the GET routes.
	if !cfg.ReadOnly {
		comparisonService.Start(ctx)
		defer comparisonService.Stop()
	}
	go func() {
		logger.Event("server_started", map[string]any{
			"address":            cfg.Address,
			"data_path":          cfg.DataPath,
			"audit_path":         cfg.AuditPath,
			"snapshot_path":      cfg.SnapshotPath,
			"comparison_path":    cfg.ComparisonPath,
			"comparison_workers": cfg.ComparisonWorkers,
			"read_only":          cfg.ReadOnly,
		})
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
