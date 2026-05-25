package handlers

import (
	"log"
	"net/http"

	"medtrack/internal/models"
)

// renderRow renders the medication-row.html partial for a single med id.
// The page mode ("full" or "focus") is taken from the `?mode=` query param so
// the row's last cell matches the page the user is viewing.
func (h *Handler) renderRow(w http.ResponseWriter, r *http.Request, id string) {
	mode := r.URL.Query().Get("mode")
	if mode != "focus" {
		mode = "full"
	}
	s := storeFromContext(r)
	diary := s.Snapshot()
	now := h.now()
	for _, m := range diary.Medications {
		if m.ID == id {
			view := buildMedicationView(m, now)
			if err := h.renderTemplateMode(w, "medication-row.html", view, mode); err != nil {
				log.Printf("row render: %v", err)
				http.Error(w, "render error", http.StatusInternalServerError)
			}
			return
		}
	}
	http.NotFound(w, r)
}

// IncrementDose handles POST /medications/{id}/dose.
func (h *Handler) IncrementDose(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "/dose")
	s := storeFromContext(r)
	if _, ok := s.RecordDose(id, h.now()); !ok {
		http.NotFound(w, r)
		return
	}
	h.renderRow(w, r, id)
}

// RevertDose handles POST /medications/{id}/dose/revert.
func (h *Handler) RevertDose(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "/dose/revert")
	s := storeFromContext(r)
	if _, ok := s.RevertDose(id, h.now()); !ok {
		http.NotFound(w, r)
		return
	}
	h.renderRow(w, r, id)
}

// AdvanceCycle handles POST /medications/{id}/cycle.
func (h *Handler) AdvanceCycle(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "/cycle")
	s := storeFromContext(r)
	if _, ok := s.AdvanceCycle(id, h.now()); !ok {
		http.NotFound(w, r)
		return
	}
	h.renderRow(w, r, id)
}

// RevertCycle handles POST /medications/{id}/cycle/revert.
func (h *Handler) RevertCycle(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "/cycle/revert")
	s := storeFromContext(r)
	if _, ok := s.RevertCycle(id, h.now()); !ok {
		http.NotFound(w, r)
		return
	}
	h.renderRow(w, r, id)
}

// MedicationLog renders the event log filtered to one medication.
// Deleted medications can still be inspected by id.
func (h *Handler) MedicationLog(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "/log")
	s := storeFromContext(r)
	diary := s.Snapshot()
	deletedMeds := diary.DeletedMedications
	deletedIDs := make(map[string]bool, len(deletedMeds))
	for _, m := range deletedMeds {
		deletedIDs[m.ID] = true
	}
	all := append(append([]models.Medication{}, diary.Medications...), deletedMeds...)
	var name string
	for _, m := range all {
		if m.ID == id {
			name = m.Name
			break
		}
	}
	if name == "" {
		http.NotFound(w, r)
		return
	}
	events := h.buildEventViews(all, id, deletedIDs, diary.Temperatures, h.now())
	if err := h.renderTemplate(w, "log.html", LogData{
		DiaryName:      diary.Name,
		Events:         events,
		FilterMed:      id,
		FilterMedName:  name,
		FilterDeleted:  deletedIDs[id],
		AllMedications: all,
	}); err != nil {
		log.Printf("log render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// Log renders the full event log across all medications (active + deleted).
func (h *Handler) Log(w http.ResponseWriter, r *http.Request) {
	s := storeFromContext(r)
	diary := s.Snapshot()
	deletedMeds := diary.DeletedMedications
	deletedIDs := make(map[string]bool, len(deletedMeds))
	for _, m := range deletedMeds {
		deletedIDs[m.ID] = true
	}
	all := append(append([]models.Medication{}, diary.Medications...), deletedMeds...)
	events := h.buildEventViews(all, "", deletedIDs, diary.Temperatures, h.now())
	chart := h.buildTemperatureChart(diary.Temperatures, all, deletedIDs)
	if err := h.renderTemplate(w, "log.html", LogData{
		DiaryName:      diary.Name,
		Events:         events,
		AllMedications: all,
		Chart:          chart,
	}); err != nil {
		log.Printf("log render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
