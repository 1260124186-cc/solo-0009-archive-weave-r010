package httpapi

import (
	"net/http"

	"example.com/solo-0009-archive-weave/internal/catalog"
)

func queryValues(r *http.Request) map[string]string {
	values := map[string]string{}
	for _, key := range []string{"q", "tag", "year", "status", "view", "sort", "offset", "limit"} {
		values[key] = r.URL.Query().Get(key)
	}
	return values
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	query, err := catalog.ParseQuery(queryValues(r))
	if err != nil {
		writeError(w, err)
		return
	}
	values, err := s.service.List(r.Context(), query)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(values),
		"query":     query.Describe(),
		"artifacts": values,
	})
}
