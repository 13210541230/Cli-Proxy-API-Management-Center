package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/seakee/cpa-manager/usage-service/internal/analytics"
)

const maxAnalyticsRequestBytes int64 = 1024 * 1024

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxAnalyticsRequestBytes)
	defer body.Close()
	var req analytics.Request
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := analytics.Query(r.Context(), s.store, req)
	if err != nil {
		var validationErr *analytics.ValidationError
		if errors.As(err, &validationErr) {
			writeError(w, http.StatusBadRequest, validationErr)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
