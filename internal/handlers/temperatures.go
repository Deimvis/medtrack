package handlers

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TemperatureData feeds the temperature.html page.
type TemperatureData struct {
	DiaryName    string
	Readings     []TemperatureRow
	DefaultLocal string // pre-filled <input type="datetime-local"> value
}

// TemperatureRow renders a single reading for the listing.
type TemperatureRow struct {
	ID          string
	ValueC      float64
	At          time.Time
	AtFormatted string
}

// Temperature renders the temperature entry page (form + list of past readings).
func (h *Handler) Temperature(w http.ResponseWriter, r *http.Request) {
	s := storeFromContext(r)
	diary := s.Snapshot()
	now := h.now()
	rows := make([]TemperatureRow, 0, len(diary.Temperatures))
	for _, t := range diary.Temperatures {
		rows = append(rows, TemperatureRow{
			ID:          t.ID,
			ValueC:      t.ValueC,
			At:          t.At,
			AtFormatted: t.At.Local().Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].At.After(rows[j].At) })

	data := TemperatureData{
		DiaryName:    diary.Name,
		Readings:     rows,
		DefaultLocal: now.Local().Format("2006-01-02T15:04"),
	}
	if err := h.renderTemplate(w, "temperature.html", data); err != nil {
		log.Printf("temperature render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// parseTemperatureInput accepts "37.2", "37,2", or "37 2" (any whitespace as
// the decimal separator), normalizes to dot, and parses it.
func parseTemperatureInput(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	// Replace commas with dots first.
	s = strings.ReplaceAll(s, ",", ".")
	// Collapse runs of whitespace and treat the first one as a decimal point.
	fields := strings.Fields(s)
	switch len(fields) {
	case 1:
		// Already a single token — accept as-is.
		s = fields[0]
	case 2:
		// "37 2" → "37.2"; only if the right side doesn't already contain a dot.
		if strings.ContainsAny(fields[0], ".") || strings.ContainsAny(fields[1], ".") {
			return 0, strconv.ErrSyntax
		}
		s = fields[0] + "." + fields[1]
	default:
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseFloat(s, 64)
}

// CreateTemperature handles POST /temperature.
func (h *Handler) CreateTemperature(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	val, err := parseTemperatureInput(r.FormValue("valueC"))
	if err != nil || val <= 0 {
		http.Error(w, "valueC must be a positive number", http.StatusBadRequest)
		return
	}
	at := h.now()
	if raw := strings.TrimSpace(r.FormValue("at")); raw != "" {
		// <input type="datetime-local"> submits "YYYY-MM-DDTHH:MM" in local time.
		if t, err := time.ParseInLocation("2006-01-02T15:04", raw, time.Local); err == nil {
			at = t
		} else if t, err := time.ParseInLocation("2006-01-02T15:04:05", raw, time.Local); err == nil {
			at = t
		}
	}
	s := storeFromContext(r)
	s.AddTemperature(at, val)
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, h.basePath+"/temperature", http.StatusSeeOther)
}

// DeleteTemperature handles POST /temperature/{id}/delete.
func (h *Handler) DeleteTemperature(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.basePath)
	path = strings.TrimPrefix(path, "/temperature/")
	id := strings.TrimSuffix(path, "/delete")
	s := storeFromContext(r)
	if !s.DeleteTemperature(id) {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, h.basePath+"/temperature", http.StatusSeeOther)
}
