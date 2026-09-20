package httpapi

import (
	"net/http"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type reviewRequest struct {
	Decision string `json:"decision"`
	Reviewer string `json:"reviewer"`
	Note     string `json:"note,omitempty"`
}

func (s *Server) reviewArtifact(w http.ResponseWriter, r *http.Request) {
	var input reviewRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, domain.Invalid("body", "invalid JSON body"))
		return
	}
	value, err := s.service.Review(r.Context(), r.PathValue("id"), input.Decision, input.Reviewer, input.Note)
	if err != nil {
		s.metrics.Failure()
		s.logger.Failure("review_artifact", err)
		writeError(w, err)
		return
	}
	s.metrics.Write()
	writeJSON(w, http.StatusOK, value)
}
