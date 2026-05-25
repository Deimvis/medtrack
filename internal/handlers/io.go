package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"medtrack/internal/models"
)

// ExportJSON returns the diary as a JSON download.
func (h *Handler) ExportJSON(w http.ResponseWriter, r *http.Request) {
	s := storeFromContext(r)
	diary := s.Snapshot()

	w.Header().Set("Content-Type", "application/json")
	safeName := strings.ReplaceAll(strings.TrimSpace(diary.Name), " ", "_")
	if safeName == "" {
		safeName = "diary"
	}
	filename := fmt.Sprintf("%s_%s.json", safeName, time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(diary); err != nil {
		log.Printf("export encode: %v", err)
	}
}

// ImportJSON replaces the diary with the uploaded JSON.
func (h *Handler) ImportJSON(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5*1024*1024))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var diary models.Diary
	if err := json.Unmarshal(body, &diary); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	for i := range diary.Medications {
		m := &diary.Medications[i]
		if m.TakingsByCycle == nil {
			m.TakingsByCycle = map[string][]time.Time{}
		}
		if m.Events == nil {
			m.Events = []models.Event{}
		}
	}
	s := storeFromContext(r)
	s.Replace(diary)
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok"))
}

// ClearState wipes the diary.
func (h *Handler) ClearState(w http.ResponseWriter, r *http.Request) {
	s := storeFromContext(r)
	s.Clear()
	h.ListMedications(w, r)
}

// ResetProgress clears all live state but keeps configuration.
func (h *Handler) ResetProgress(w http.ResponseWriter, r *http.Request) {
	s := storeFromContext(r)
	s.ResetProgress()
	h.ListMedications(w, r)
}
