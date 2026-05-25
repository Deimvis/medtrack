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
	// Server-side fallback filename (used only if JS is disabled — otherwise
	// the client builds the filename in the visitor's local timezone). Uses
	// UTC with no tz suffix.
	filename := fmt.Sprintf("%s_%s.json", safeName, time.Now().UTC().Format("20060102_150405"))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(diary); err != nil {
		log.Printf("export encode: %v", err)
	}
}

// ImportJSON replaces the diary with the uploaded JSON. The parser is
// tolerant: any JSON file is accepted. If the file doesn't match our schema
// at all, the diary is reset to an empty state (so the user at least lands
// on a clean slate instead of a 400).
func (h *Handler) ImportJSON(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5*1024*1024))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	diary, perr := parseDiaryLoose(body)
	if perr != nil {
		http.Error(w, "invalid JSON: "+perr.Error(), http.StatusBadRequest)
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

// parseDiaryLoose parses a JSON payload into a Diary. It first tries the
// strict schema; if anything goes wrong (or the data has nothing recognisable),
// it falls back to a generic walk that extracts as much as possible without
// failing. Returns an error only when the payload isn't valid JSON at all.
func parseDiaryLoose(body []byte) (models.Diary, error) {
	// Strict path: matches what we export, the common case.
	var strict models.Diary
	if err := json.Unmarshal(body, &strict); err == nil {
		return strict, nil
	}

	// Loose path: walk the JSON. Accept any valid JSON, extract what we can.
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return models.Diary{}, err
	}
	diary := models.Diary{
		Name:        "Imported diary",
		Medications: []models.Medication{},
	}
	applyMapToDiary(&diary, raw)
	return diary, nil
}

// applyMapToDiary tries to fill diary from arbitrary JSON. Top-level can be
// either our diary shape, an array of medications, or just unrelated data.
func applyMapToDiary(diary *models.Diary, raw interface{}) {
	switch v := raw.(type) {
	case map[string]interface{}:
		if n, ok := v["name"].(string); ok && strings.TrimSpace(n) != "" {
			diary.Name = n
		}
		if meds, ok := v["medications"].([]interface{}); ok {
			diary.Medications = parseMedsLoose(meds)
		}
		if dels, ok := v["deletedMedications"].([]interface{}); ok {
			diary.DeletedMedications = parseMedsLoose(dels)
		}
		if temps, ok := v["temperatures"].([]interface{}); ok {
			diary.Temperatures = parseTempsLoose(temps)
		}
	case []interface{}:
		// Top-level array: treat as medications.
		diary.Medications = parseMedsLoose(v)
	}
}

func parseMedsLoose(items []interface{}) []models.Medication {
	out := make([]models.Medication, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue // skip nameless entries
		}
		// Re-marshal and unmarshal: lets json.Unmarshal handle the typed
		// fields (RangeInt, time.Time, etc.) without us reimplementing them.
		// We tolerate failure of individual fields by zero-ing them.
		buf, _ := json.Marshal(m)
		var med models.Medication
		_ = json.Unmarshal(buf, &med)
		med.Name = name
		if med.TakingsByCycle == nil {
			med.TakingsByCycle = map[string][]time.Time{}
		}
		if med.Events == nil {
			med.Events = []models.Event{}
		}
		out = append(out, med)
	}
	return out
}

func parseTempsLoose(items []interface{}) []models.TemperatureEvent {
	out := make([]models.TemperatureEvent, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		buf, _ := json.Marshal(m)
		var t models.TemperatureEvent
		if err := json.Unmarshal(buf, &t); err != nil {
			continue
		}
		if t.At.IsZero() && t.ValueC == 0 {
			continue
		}
		out = append(out, t)
	}
	return out
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
