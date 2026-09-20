package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"example.com/solo-0009-archive-weave/internal/domain"
)

const actorHeader = "X-Archive-Actor"

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

func requestActor(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(actorHeader))
}

func (s *Server) writable(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.readOnly {
			writeError(w, domain.Conflict("service is running in read-only mode"))
			return
		}
		next(w, r)
	}
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.metrics.Failure()
				s.logger.Event("request_panic", map[string]any{
					"path":  r.URL.Path,
					"value": recovered,
				})
				writeError(w, domain.Conflict("request could not be completed"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
