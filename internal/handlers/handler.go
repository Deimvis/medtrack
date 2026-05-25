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
	StatusTooltip    string // human-readable status sentence used as the LED's tooltip
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
	v.Status, v.StatusTooltip = computeStatus(m, now)
	return v
}

// computeStatus determines the row's status colour key.
// done: minTarget reached (uses PerCycle.Min — softest limit).
// none: no interval configured and not done.
// ready: target exists, no prior taking yet.
// early/ontime/late: based on interval since last taking.
func computeStatus(m models.Medication, now time.Time) (string, string) {
	usedInCycle := len(m.TakingsForCurrentCycle())
	maxTarget := m.PerCycle.Max
	if maxTarget > 0 && usedInCycle >= maxTarget {
		return "done", "Cycle max reached — no more doses needed."
	}
	if m.Interval.IsZero() {
		return "none", "No interval configured — status unavailable."
	}
	last := latestInCurrentOrEarlier(m)
	if last.IsZero() {
		return "ready", "No dose taken yet — ready to be used."
	}
	since := now.Sub(last)
	minDur := time.Duration(m.Interval.MinHours * float64(time.Hour))
	maxDur := time.Duration(m.Interval.MaxHours * float64(time.Hour))
	hasUpper := m.Interval.MaxHours > 0
	intervalLine := fmt.Sprintf("Interval: %s", intervalLabel(m.Interval))
	switch {
	case since < minDur:
		return "early", fmt.Sprintf(
			"It is early to be used (time left: %s).\n%s",
			formatDurationShort(minDur-since), intervalLine,
		)
	case !hasUpper:
		// Only the lower bound is configured — past min there is no late
		// state, the medication is just "ready to take" indefinitely.
		return "ontime", fmt.Sprintf(
			"It is right time to be used (no upper bound).\n%s",
			intervalLine,
		)
	case since <= maxDur:
		return "ontime", fmt.Sprintf(
			"It is right time to be used (time left: %s).\n%s",
			formatDurationShort(maxDur-since), intervalLine,
		)
	default:
		return "late", fmt.Sprintf(
			"It is late to be used (overdue by: %s).\n%s",
			formatDurationShort(since-maxDur), intervalLine,
		)
	}
}

// formatDurationShort renders a duration as "Xd YYh", "Xh YYm", or "XXm".
func formatDurationShort(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		return "less than 1m"
	}
	days := int(d / (24 * time.Hour))
	hours := int(d % (24 * time.Hour) / time.Hour)
	minutes := int(d % time.Hour / time.Minute)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
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
	// MaxHours == 0 with MinHours > 0 means "no upper bound" — render as "≥ X h".
	if i.MaxHours == 0 {
		return fmt.Sprintf("≥ %g h", i.MinHours)
	}
	if i.MinHours == i.MaxHours {
		return fmt.Sprintf("%g h", i.MinHours)
	}
	return fmt.Sprintf("%g–%g h", i.MinHours, i.MaxHours)
}

// formatTakingTimestamp returns the absolute timestamp in RFC3339 form so
// the client can render it in the visitor's local timezone. Returns "" for
// the zero time.
func formatTakingTimestamp(t, _ time.Time) string { return isoTS(t) }

// isoTS returns t as an RFC3339 string with an explicit offset. We always
// emit a fully-qualified absolute timestamp so the client can convert it to
// the user's local timezone unambiguously.
func isoTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
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
				AtFormatted:    isoTS(e.At),
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
				AtFormatted:  isoTS(t.At),
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
