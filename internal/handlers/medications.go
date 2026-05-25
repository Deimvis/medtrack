package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"medtrack/internal/models"
)

// Index renders the main diary page.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != h.basePath+"/" && r.URL.Path != "/" && r.URL.Path != h.basePath {
		http.NotFound(w, r)
		return
	}
	s := storeFromContext(r)
	diary := s.Snapshot()
	now := h.now()

	views := make([]MedicationView, 0, len(diary.Medications))
	for _, m := range diary.Medications {
		views = append(views, buildMedicationView(m, now))
	}
	data := IndexData{DiaryName: diary.Name, Medications: views}
	if err := h.renderTemplate(w, "index.html", data); err != nil {
		log.Printf("index render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// ListMedications renders just the table partial.
func (h *Handler) ListMedications(w http.ResponseWriter, r *http.Request) {
	s := storeFromContext(r)
	diary := s.Snapshot()
	now := h.now()
	views := make([]MedicationView, 0, len(diary.Medications))
	for _, m := range diary.Medications {
		views = append(views, buildMedicationView(m, now))
	}
	if err := h.renderTemplate(w, "medication-table.html", IndexData{
		DiaryName: diary.Name, Medications: views,
	}); err != nil {
		log.Printf("list render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// medicationFromForm parses the create/update form into a Medication.
func medicationFromForm(r *http.Request) models.Medication {
	parseInt := func(s string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		return n
	}
	parseFloat := func(s string) float64 {
		f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f
	}
	rangeFromForm := func(minKey, maxKey string) models.RangeInt {
		minV := parseInt(r.FormValue(minKey))
		maxV := parseInt(r.FormValue(maxKey))
		if minV == 0 && maxV != 0 {
			minV = maxV
		}
		if maxV == 0 && minV != 0 {
			maxV = minV
		}
		return models.RangeInt{Min: minV, Max: maxV}
	}

	m := models.Medication{
		Name:     strings.TrimSpace(r.FormValue("name")),
		PerCycle: rangeFromForm("perCycleMin", "perCycleMax"),
		CycleDuration: models.CycleDuration{
			Value: parseFloat(r.FormValue("cycleValue")),
			Unit:  strings.TrimSpace(r.FormValue("cycleUnit")),
		},
		CyclesTotal: rangeFromForm("cyclesTotalMin", "cyclesTotalMax"),
		Interval: intervalFromForm(
			parseFloat(r.FormValue("intervalMin")),
			parseFloat(r.FormValue("intervalMax")),
		),
	}
	if m.CycleDuration.Value > 0 && m.CycleDuration.Unit == "" {
		m.CycleDuration.Unit = "day"
	}
	return m
}

// intervalFromForm collapses a "range" interval into a fixed value when the
// user supplied only one of the two endpoints. If both are non-zero it keeps
// them as-is; if only one is set, the other mirrors it; if both are zero, the
// interval is treated as not configured.
func intervalFromForm(minH, maxH float64) models.IntervalHours {
	if minH == 0 && maxH != 0 {
		minH = maxH
	}
	if maxH == 0 && minH != 0 {
		maxH = minH
	}
	return models.IntervalHours{MinHours: minH, MaxHours: maxH}
}

// CreateMedication handles POST /medications.
func (h *Handler) CreateMedication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	m := medicationFromForm(r)
	if m.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	s := storeFromContext(r)
	s.Create(m)
	h.ListMedications(w, r)
}

// extractMedID pulls {id} out of /medications/{id}[/...].
func (h *Handler) extractMedID(r *http.Request, suffix string) string {
	path := strings.TrimPrefix(r.URL.Path, h.basePath)
	path = strings.TrimPrefix(path, "/medications/")
	if suffix != "" {
		path = strings.TrimSuffix(path, suffix)
	}
	return strings.Trim(path, "/")
}

// UpdateMedication handles POST /medications/{id}.
func (h *Handler) UpdateMedication(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	m := medicationFromForm(r)
	if m.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	s := storeFromContext(r)
	if !s.Update(id, m) {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, h.basePath+"/", http.StatusSeeOther)
}

// DeleteMedication handles DELETE /medications/{id}.
func (h *Handler) DeleteMedication(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "")
	s := storeFromContext(r)
	if !s.Delete(id) {
		http.NotFound(w, r)
		return
	}
	h.ListMedications(w, r)
}

// EditMedication renders the edit form for a single medication.
func (h *Handler) EditMedication(w http.ResponseWriter, r *http.Request) {
	id := h.extractMedID(r, "/edit")
	s := storeFromContext(r)
	diary := s.Snapshot()
	var found *models.Medication
	for i := range diary.Medications {
		if diary.Medications[i].ID == id {
			found = &diary.Medications[i]
			break
		}
	}
	if found == nil {
		http.NotFound(w, r)
		return
	}
	if err := h.renderTemplate(w, "edit-medication.html", found); err != nil {
		log.Printf("edit render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// UpdateDiaryName handles POST /diary/name.
func (h *Handler) UpdateDiaryName(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	s := storeFromContext(r)
	s.SetName(r.FormValue("name"))
	w.WriteHeader(http.StatusNoContent)
}
