package httpapi

import (
	"net/http"
	"time"

	"example.com/solo-0009-archive-weave/internal/catalog"
	"example.com/solo-0009-archive-weave/internal/comparison"
	"example.com/solo-0009-archive-weave/internal/observability"
)

type Server struct {
	service     *catalog.Service
	comparisons *comparison.Manager
	logger      *observability.Logger
	metrics     *observability.Metrics
	mux         *http.ServeMux
	readOnly    bool
}

func NewServer(service *catalog.Service, comparisons *comparison.Manager,
	logger *observability.Logger, metrics *observability.Metrics, readOnly bool) *Server {
	server := &Server{
		service:     service,
		comparisons: comparisons,
		logger:      logger,
		metrics:     metrics,
		mux:         http.NewServeMux(),
		readOnly:    readOnly,
	}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return withRequestID(s.logging(s.recoverPanic(s.mux)))
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.metrics.Request()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		s.logger.Request(r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
