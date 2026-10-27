package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"thura/internal/dmarcreports"
	"thura/internal/setup"
)

func ReceiveDMARCReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !setup.PrivatePostfixRequest(r) {
		http.Error(w, "Private mail service authentication required", http.StatusForbidden)
		return
	}
	encoded := r.Header.Get("X-Thura-Report-Recipients")
	var recipients []string
	if len(encoded) > 4096 || json.Unmarshal([]byte(encoded), &recipients) != nil {
		http.Error(w, "Invalid report recipients", http.StatusBadRequest)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		http.Error(w, "Report exceeds size limit", http.StatusRequestEntityTooLarge)
		return
	}
	id, err := dmarcreports.Queue(r.Context(), raw, recipients)
	if err != nil {
		http.Error(w, "Aggregate report could not be queued", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "queued-or-already-submitted"}); err != nil {
		slog.WarnContext(r.Context(), "DMARC report acknowledgement could not be written")
	}
}
