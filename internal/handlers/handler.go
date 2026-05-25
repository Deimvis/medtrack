package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"medtrack/internal/models"
)

// Handler holds template state shared across requests.
type Handler struct {
	templates *template.Template
	basePath  string
	NowFunc   func() time.Time
}

func NewHandler(basePath string) *Handler {
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "../..")
	templatePath := filepath.Join(projectRoot, "internal/templates/*.html")

	toFloat := func(v interface{}) float64 {
		switch x := v.(type) {
		case float64:
			return x
		case float32:
			return float64(x)
		case int:
			return float64(x)
		case int64:
			return float64(x)
		case int32:
			return float64(x)
		}
		return 0
	}
	funcMap := template.FuncMap{
		"add": func(a, b interface{}) float64 { return toFloat(a) + toFloat(b) },
		"sub": func(a, b interface{}) float64 { return toFloat(a) - toFloat(b) },
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of arguments")
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob(templatePath))
	return &Handler{
		templates: tmpl,
		basePath:  basePath,
		NowFunc:   time.Now,
	}
}

func (h *Handler) now() time.Time { return h.NowFunc() }

func (h *Handler) renderTemplate(w http.ResponseWriter, name string, data any) error {
	return h.renderTemplateMode(w, name, data, "")
}

// renderTemplateMode is like renderTemplate but also exposes a top-level Mode
// field used by partials that change shape based on the page context (e.g.
// medication-table.html on the Focus page renders a Completion column instead
// of Actions).
func (h *Handler) renderTemplateMode(w http.ResponseWriter, name string, data any, mode string) error {
	wrapped := struct {
		Data     any
		BasePath string
		Mode     string
	}{Data: data, BasePath: h.basePath, Mode: mode}

	var buf bytes.Buffer
	if err := h.templates.ExecuteTemplate(&buf, name, wrapped); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// MedicationView is the per-row view-model passed to templates.
type MedicationView struct {
	models.Medication
	UsedInCycle      int
	PerCycleLabel    string
	CycleLabel       string
	CyclesTotalLabel string
	IntervalLabel    string
	FirstInCycle     string
	LastInCycle      string
	Status           string // early|ontime|late|ready|done|none
	IntervalWarning  string // non-empty when the config is inconsistent

	// CompletionPct is the average per-cycle adherence as a percentage (0–100),
	// computed against PerCycle.Min across all cycles up to and including the
	// current one. CompletionAvailable is false when PerCycle.Min == 0.
	CompletionPct       int
	CompletionAvailable bool
	CompletionColor     string // "green"|"yellow"|"orange"|"red"|"" (no tint when no cycle has passed)
}

// EventViewType is a string covering both medication EventType values and
// the synthetic "temperature" kind used by the log view.
type EventViewType string

const EventViewTemperature EventViewType = "temperature"

// EventView is the rendered form of one event for the log page.
type EventView struct {
	MedicationID   string
	MedicationName string
	MedicationDel  bool // true when the source medication has been deleted
	At             time.Time
	AtFormatted    string
	Type           EventViewType
	TypeLabel      string
	CycleIndex     int
	TakingAt       *time.Time
	TakingFmt      string
	TemperatureC   float64
}

// IndexData is the data shape for index.html.
type IndexData struct {
	DiaryName   string
	Medications []MedicationView
}

// LogData is the data shape for log.html.
type LogData struct {
	DiaryName      string
	Events         []EventView
	FilterMed      string // pre-selected medication id or ""
	FilterMedName  string // when filtered to one medication, its name
	FilterDeleted  bool   // true when the filtered medication has been deleted
	AllMedications []models.Medication
	Chart          ChartData
}

// buildMedicationView assembles the view-model for a single medication.
func buildMedicationView(m models.Medication, now time.Time) MedicationView {
	takings := append([]time.Time(nil), m.TakingsForCurrentCycle()...)
	sort.Slice(takings, func(i, j int) bool { return takings[i].Before(takings[j]) })
	usedInCycle := len(takings)

	v := MedicationView{
		Medication:       m,
		UsedInCycle:      usedInCycle,
		PerCycleLabel:    rangeLabel(m.PerCycle, ""),
		CycleLabel:       cycleLabel(m.CycleDuration),
		CyclesTotalLabel: rangeLabel(m.CyclesTotal, ""),
		IntervalLabel:    intervalLabel(m.Interval),
		IntervalWarning:  detectIntervalWarning(m),
	}
	if len(takings) > 0 {
		v.FirstInCycle = formatTakingTimestamp(takings[0], now)
		v.LastInCycle = formatTakingTimestamp(takings[len(takings)-1], now)
	}

	// Completion: average per-cycle adherence against PerCycle.Min, across all
	// cycles up to and including the current one. A cycle is "satisfied" when
	// its takings count >= PerCycle.Min (overflow caps at 100%).
	if m.PerCycle.Min > 0 {
		target := float64(m.PerCycle.Min)
		var sum float64
		total := m.CycleIndex + 1 // includes current cycle
		for i := 0; i < total; i++ {
			used := float64(len(m.TakingsForCycle(i)))
			pct := used / target * 100
			if pct > 100 {
				pct = 100
			}
			sum += pct
		}
		avg := sum / float64(total)
		v.CompletionAvailable = true
		v.CompletionPct = int(avg + 0.5) // round to nearest
		// Only color once at least one cycle has been completed.
		if m.CycleIndex > 0 {
			switch {
			case v.CompletionPct >= 90:
				v.CompletionColor = "green"
			case v.CompletionPct >= 70:
				v.CompletionColor = "yellow"
			case v.CompletionPct >= 50:
				v.CompletionColor = "orange"
			default:
				v.CompletionColor = "red"
			}
		}
	}
	v.Status = computeStatus(m, now)
	return v
}

// computeStatus determines the row's status colour key.
// done: minTarget reached (uses PerCycle.Min — softest limit).
// none: no interval configured and not done.
// ready: target exists, no prior taking yet.
// early/ontime/late: based on interval since last taking.
func computeStatus(m models.Medication, now time.Time) string {
	usedInCycle := len(m.TakingsForCurrentCycle())
	minTarget := m.PerCycle.Min
	if minTarget > 0 && usedInCycle >= minTarget {
		return "done"
	}
	if m.Interval.IsZero() {
		return "none"
	}
	last := latestInCurrentOrEarlier(m)
	if last.IsZero() {
		return "ready"
	}
	since := now.Sub(last).Hours()
	switch {
	case since < m.Interval.MinHours:
		return "early"
	case since <= m.Interval.MaxHours:
		return "ontime"
	default:
		return "late"
	}
}

func latestInCurrentOrEarlier(m models.Medication) time.Time {
	cur := m.TakingsForCurrentCycle()
	if len(cur) > 0 {
		latest := cur[0]
		for _, t := range cur[1:] {
			if t.After(latest) {
				latest = t
			}
		}
		return latest
	}
	return m.LastTakingAny()
}

// detectIntervalWarning returns a human-readable warning when the configured
// interval is inconsistent with the per-cycle dose target.
// cycleHours / Interval.MinHours < PerCycle.Max triggers it.
func detectIntervalWarning(m models.Medication) string {
	if m.Interval.IsZero() || m.Interval.MinHours == 0 || m.PerCycle.Max == 0 {
		return ""
	}
	cycleHours := m.CycleDuration.Hours()
	maxFit := cycleHours / m.Interval.MinHours
	if maxFit < float64(m.PerCycle.Max) {
		return fmt.Sprintf(
			"Interval X=%g h fits at most %g doses per %g h cycle, but per-cycle max is %d.",
			m.Interval.MinHours, maxFit, cycleHours, m.PerCycle.Max,
		)
	}
	return ""
}

func rangeLabel(r models.RangeInt, blank string) string {
	if r.IsZero() {
		return blank
	}
	if r.IsScalar() {
		return fmt.Sprintf("%d", r.Min)
	}
	return fmt.Sprintf("%d–%d", r.Min, r.Max)
}

func cycleLabel(c models.CycleDuration) string {
	if c.IsZero() {
		return ""
	}
	unit := c.Unit
	if unit == "" {
		unit = "day"
	}
	if c.Value == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%g %ss", c.Value, unit)
}

func intervalLabel(i models.IntervalHours) string {
	if i.IsZero() {
		return ""
	}
	return fmt.Sprintf("%g–%g h", i.MinHours, i.MaxHours)
}

// formatTakingTimestamp formats t relative to now with words today/yesterday.
func formatTakingTimestamp(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	tLocal := t.Local()
	nowLocal := now.Local()
	day := func(x time.Time) time.Time {
		return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location())
	}
	deltaDays := int(day(nowLocal).Sub(day(tLocal)).Hours() / 24)
	hhmm := tLocal.Format("15:04")
	switch deltaDays {
	case 0:
		return "today " + hhmm
	case 1:
		return "yesterday " + hhmm
	default:
		return hhmm + ", " + tLocal.Format("2006-01-02")
	}
}

// eventTypeLabel returns a human-readable name for an event type.
func eventTypeLabel(t models.EventType) string {
	switch t {
	case models.EventDose:
		return "Dose taken"
	case models.EventDoseRevert:
		return "Dose reverted"
	case models.EventCycleAdvance:
		return "Cycle advanced"
	case models.EventCycleRevert:
		return "Cycle reverted"
	default:
		return string(t)
	}
}

// buildEventViews flattens events from one or all medications (and optional
// temperature readings) into a single chronological list (newest first).
// IDs in deletedIDs are flagged so the log template can mark them as deleted.
// Temperature readings are skipped when filterID is non-empty (the per-med log).
func (h *Handler) buildEventViews(meds []models.Medication, filterID string, deletedIDs map[string]bool, temps []models.TemperatureEvent, now time.Time) []EventView {
	var out []EventView
	for _, m := range meds {
		if filterID != "" && m.ID != filterID {
			continue
		}
		isDel := deletedIDs[m.ID]
		for _, e := range m.Events {
			ev := EventView{
				MedicationID:   m.ID,
				MedicationName: m.Name,
				MedicationDel:  isDel,
				At:             e.At,
				AtFormatted:    e.At.Local().Format("2006-01-02 15:04:05"),
				Type:           EventViewType(e.Type),
				TypeLabel:      eventTypeLabel(e.Type),
				CycleIndex:     e.CycleIndex,
				TakingAt:       e.TakingAt,
			}
			if e.TakingAt != nil {
				ev.TakingFmt = formatTakingTimestamp(*e.TakingAt, now)
			}
			out = append(out, ev)
		}
	}
	if filterID == "" {
		for _, t := range temps {
			recorded := t.At
			out = append(out, EventView{
				At:           t.At,
				AtFormatted:  t.At.Local().Format("2006-01-02 15:04:05"),
				Type:         EventViewTemperature,
				TypeLabel:    "Temperature",
				TemperatureC: t.ValueC,
				TakingAt:     &recorded,
				TakingFmt:    formatTakingTimestamp(t.At, now),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}
