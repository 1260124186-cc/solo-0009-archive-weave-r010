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
	service := catalog.NewService(repository, auditRepository)
	handler := httpapi.NewServer(service, logger, metrics, cfg.ReadOnly).Handler()
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
			"address":    cfg.Address,
			"data_path":  cfg.DataPath,
			"audit_path": cfg.AuditPath,
			"read_only":  cfg.ReadOnly,
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
