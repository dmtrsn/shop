package httpapi

import (
	"net/http"
	"sweetshop/internal/domain"
)

func (s *Server) repeatPreviewV2(w http.ResponseWriter, r *http.Request) {
	preview, err := s.service.RepeatPreviewV2(r.Context(), currentUser(r).ID, r.PathValue("order"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) repeatCommitV2(w http.ResponseWriter, r *http.Request) {
	var input domain.RepeatV2Input
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	keys := r.Header.Values(idempotencyKeyHeader)
	if len(keys) != 1 {
		s.writeError(w, r, &domain.ValidationError{Fields: []domain.FieldError{{Field: idempotencyKeyHeader, Message: "Передайте ровно один Idempotency-Key"}}})
		return
	}
	input.IdempotencyKey = keys[0]
	result, err := s.service.CommitRepeatV2(r.Context(), currentUser(r).ID, r.PathValue("order"), input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusOK, result)
}
