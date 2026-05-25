package handlers

import (
	"log"
	"net/http"

	"medtrack/internal/models"
)

// FocusData is the data shape for the Focus page (focus.html).
type FocusData struct {
	DiaryName    string
	Medications  []MedicationView
	DefaultLocal string
	Chart        ChartData
	Events       []EventView
}

// buildFocusData assembles the medication views, chart, and event log used by
// both Focus (full page) and FocusLogSection (HTMX-swappable partial).
func (h *Handler) buildFocusData(r *http.Request) FocusData {
	s := storeFromContext(r)
	diary := s.Snapshot()
	now := h.now()

	views := make([]MedicationView, 0, len(diary.Medications))
	for _, m := range diary.Medications {
		views = append(views, buildMedicationView(m, now))
	}

	deletedMeds := diary.DeletedMedications
	deletedIDs := make(map[string]bool, len(deletedMeds))
	for _, m := range deletedMeds {
		deletedIDs[m.ID] = true
	}
	all := append(append([]models.Medication{}, diary.Medications...), deletedMeds...)
	events := h.buildEventViews(all, "", deletedIDs, diary.Temperatures, now)
	chart := h.buildTemperatureChart(diary.Temperatures, all, deletedIDs)

	return FocusData{
		DiaryName:    diary.Name,
		Medications:  views,
		DefaultLocal: now.Local().Format("2006-01-02T15:04"),
		Chart:        chart,
		Events:       events,
	}
}

// Focus renders an "everything in one place" page that reuses the medication
// table, temperature record form, and event log (chart + table) — minus any
// configuration UI (no add-medication form, no clear/reset/export controls).
func (h *Handler) Focus(w http.ResponseWriter, r *http.Request) {
	data := h.buildFocusData(r)
	if err := h.renderTemplate(w, "focus.html", data); err != nil {
		log.Printf("focus render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// FocusLogSection returns the chart + event-log partial as HTML, wrapped in
// the same #focus-log container the Focus page swaps. Used after any
// HTMX-triggered state change on the Focus page so the chart and log refresh.
func (h *Handler) FocusLogSection(w http.ResponseWriter, r *http.Request) {
	data := h.buildFocusData(r)
	partial := struct {
		BasePath  string
		Chart     ChartData
		Events    []EventView
		ShowChart bool
	}{
		BasePath:  "", // resolved via wrapper template inputs; chart template uses dict
		Chart:     data.Chart,
		Events:    data.Events,
		ShowChart: true,
	}
	// We render via the shared "focus-log-wrapper.html" so the swap target id
	// is preserved inside the response.
	if err := h.renderTemplate(w, "focus-log-wrapper.html", partial); err != nil {
		log.Printf("focus log section render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
