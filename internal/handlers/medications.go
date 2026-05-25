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
	// optInt returns (value, present). An empty field reports present=false
	// so we can distinguish "not filled" from an explicit "0".
	optInt := func(key string) (int, bool) {
		raw := strings.TrimSpace(r.FormValue(key))
		if raw == "" {
			return 0, false
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	optFloat := func(key string) (float64, bool) {
		raw := strings.TrimSpace(r.FormValue(key))
		if raw == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	rangeFromForm := func(minKey, maxKey string) models.RangeInt {
		minV, minOK := optInt(minKey)
		maxV, maxOK := optInt(maxKey)
		switch {
		case !minOK && !maxOK:
			return models.RangeInt{}
		case minOK && !maxOK:
			return models.RangeInt{Min: minV, Max: minV}
		case !minOK && maxOK:
			return models.RangeInt{Min: maxV, Max: maxV}
		default:
			return models.RangeInt{Min: minV, Max: maxV}
		}
	}

	cycleVal, _ := optFloat("cycleValue")
	intervalMin, _ := optFloat("intervalMin")
	intervalMax, _ := optFloat("intervalMax")

	m := models.Medication{
		Name:     strings.TrimSpace(r.FormValue("name")),
		PerCycle: rangeFromForm("perCycleMin", "perCycleMax"),
		CycleDuration: models.CycleDuration{
			Value: cycleVal,
			Unit:  strings.TrimSpace(r.FormValue("cycleUnit")),
		},
		CyclesTotal: rangeFromForm("cyclesTotalMin", "cyclesTotalMax"),
		Interval:    intervalFromForm(intervalMin, intervalMax),
	}
	if m.CycleDuration.Value > 0 && m.CycleDuration.Unit == "" {
		m.CycleDuration.Unit = "day"
	}
	return m
}

// intervalFromForm normalises an interval. Only the min-hours endpoint can be
// supplied alone (meaning "no upper bound" — no late state). Supplying only
// the max-hours endpoint is not allowed; that combination is rejected by the
// caller. If both are zero, the interval is treated as not configured.
func intervalFromForm(minH, maxH float64) models.IntervalHours {
	return models.IntervalHours{MinHours: minH, MaxHours: maxH}
}

// validateMedication returns a user-readable error if the medication form
// values are inconsistent, or "" when everything is fine. It inspects the
// raw request so it can tell "0" apart from "field left blank".
func validateMedication(r *http.Request, m models.Medication) string {
	if m.Name == "" {
		return "name is required"
	}
	minRaw := strings.TrimSpace(r.FormValue("intervalMin"))
	maxRaw := strings.TrimSpace(r.FormValue("intervalMax"))
	// Dose interval: only-max is not allowed (the user has to set the lower
	// bound — an upper bound on its own has no meaning here). Note that an
	// explicit "0" counts as a valid lower bound.
	if minRaw == "" && maxRaw != "" {
		return "Dose interval: please set the lower bound (min hours). " +
			"Specifying only the upper bound is not allowed."
	}
	return ""
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
	if msg := validateMedication(r, m); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
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
	if msg := validateMedication(r, m); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
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
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		// Refuse to clear the title — keep the previous value.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s := storeFromContext(r)
	s.SetName(name)
	w.WriteHeader(http.StatusNoContent)
}
