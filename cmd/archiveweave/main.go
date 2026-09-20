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
	"example.com/solo-0009-archive-weave/internal/comparison"
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

	snapshots := storage.NewJSONSnapshotStore(cfg.SnapshotPath)
	repository := storage.NewSnapshottingRepository(storage.NewJSONStore(cfg.DataPath), snapshots)
	auditRepository := storage.NewJSONAuditStore(cfg.AuditPath)
	service := catalog.NewService(repository, auditRepository)

	jobStore := comparison.NewJSONJobStore(cfg.ComparisonPath)
	manager := comparison.NewManager(jobStore, snapshots, comparison.Options{
		Concurrency: cfg.ComparisonConcurrent,
	}, logger, metrics)

	rootContext, stopRoot := context.WithCancel(context.Background())
	defer stopRoot()
	manager.Start(rootContext)

	handler := httpapi.NewServer(service, manager, logger, metrics, cfg.ReadOnly).Handler()
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
	go func() {
		logger.Event("server_started", map[string]any{
			"address":                cfg.Address,
			"data_path":              cfg.DataPath,
			"audit_path":             cfg.AuditPath,
			"snapshot_path":          cfg.SnapshotPath,
			"comparison_path":        cfg.ComparisonPath,
			"comparison_concurrency": cfg.ComparisonConcurrent,
			"read_only":              cfg.ReadOnly,
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
	// HTTP 已停止接收新请求，等待在途比较项目落盘后再退出。
	closeContext, closeCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer closeCancel()
	if err := manager.Close(closeContext); err != nil {
		log.Printf("comparison manager shutdown error: %v", err)
	}
}
