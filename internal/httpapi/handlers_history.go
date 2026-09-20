package httpapi

import "net/http"

func (s *Server) artifactHistory(w http.ResponseWriter, r *http.Request) {
	timeline, err := s.service.History(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, timeline)
}

func (s *Server) catalogSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.service.Summary(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
