package handlers

import (
	"fmt"
	"math"
	"sort"
	"time"

	"medtrack/internal/models"
)

// Chart geometry constants. The SVG uses a logical viewBox so the chart is
// resolution-independent; CSS makes it span the container width.
const (
	chartW       = 720
	chartH       = 240
	chartLeftPx  = 46  // room for °C tick labels
	chartRightPx = 12
	chartTopPx   = 16
	chartBottomP = 28 // room for time tick labels
)

// ChartData feeds the SVG temperature chart at the top of /log.
type ChartData struct {
	Empty       bool // true ⇒ no data, template should show a placeholder
	W, H        int
	Plot        struct{ X1, Y1, X2, Y2 float64 } // plot box bounds in viewBox units
	YTicks      []ChartYTick
	XTicks      []ChartXTick
	Points      []ChartPoint
	PolylinePts string         // "x1,y1 x2,y2 …" for <polyline points="…">
	Markers     []ChartMarker  // vertical lines for dose events
	Legend      []ChartLegend  // medication name + colour swatch
}

type ChartYTick struct {
	Y       float64
	ValueC  float64
	Label   string
}

type ChartXTick struct {
	X     float64
	Label string // legacy/fallback; client replaces this using TS + Format
	TS    string // RFC3339 absolute time at this tick
	Fmt   string // hint for the client: "HH:MM" | "MM-DD" | "YYYY-MM"
}

type ChartPoint struct {
	X, Y          float64
	ValueC        float64
	TooltipPrefix string // e.g. "37.4 °C — " (timestamp appended client-side)
	TooltipTS     string // RFC3339 timestamp the client formats locally
}

type ChartMarker struct {
	X             float64
	YTop          float64
	YBottom       float64
	Color         string
	TooltipPrefix string // e.g. "Aspirin — " (timestamp appended client-side)
	TooltipTS     string // RFC3339 timestamp the client formats locally
}

type ChartLegend struct {
	Name  string
	Color string
}

// medicationColor returns a stable color from a small palette based on index.
func medicationColor(idx int) string {
	palette := []string{
		"#2c7be5", "#d97706", "#0f766e", "#a21caf",
		"#dc2626", "#0891b2", "#65a30d", "#9333ea",
	}
	return palette[idx%len(palette)]
}

// buildTemperatureChart constructs all geometry for the SVG chart.
func (h *Handler) buildTemperatureChart(temps []models.TemperatureEvent, meds []models.Medication, deletedIDs map[string]bool) ChartData {
	chart := ChartData{W: chartW, H: chartH}

	// Collect dose events from all medications, in chronological order, with
	// stable per-medication colors.
	type doseMark struct {
		at      time.Time
		medID   string
		medName string
		color   string
		deleted bool
	}
	var doses []doseMark
	colorOf := map[string]string{}
	nameOf := map[string]string{}
	medIDs := make([]string, 0, len(meds))
	for _, m := range meds {
		if _, ok := colorOf[m.ID]; !ok {
			colorOf[m.ID] = medicationColor(len(medIDs))
			nameOf[m.ID] = m.Name
			medIDs = append(medIDs, m.ID)
		}
		for _, e := range m.Events {
			if e.Type != models.EventDose {
				continue
			}
			doses = append(doses, doseMark{
				at:      e.At,
				medID:   m.ID,
				medName: m.Name,
				color:   colorOf[m.ID],
				deleted: deletedIDs[m.ID],
			})
		}
	}

	if len(temps) == 0 && len(doses) == 0 {
		chart.Empty = true
		return chart
	}

	// Determine the time domain.
	var tMin, tMax time.Time
	first := true
	consider := func(t time.Time) {
		if first {
			tMin, tMax = t, t
			first = false
			return
		}
		if t.Before(tMin) {
			tMin = t
		}
		if t.After(tMax) {
			tMax = t
		}
	}
	for _, t := range temps {
		consider(t.At)
	}
	for _, d := range doses {
		consider(d.at)
	}
	if tMin.Equal(tMax) {
		// Single instant — pad by ±30 min so we have a real range to draw on.
		tMin = tMin.Add(-30 * time.Minute)
		tMax = tMax.Add(30 * time.Minute)
	} else {
		// 5% padding either side.
		span := tMax.Sub(tMin)
		pad := time.Duration(float64(span) * 0.05)
		if pad < time.Minute {
			pad = time.Minute
		}
		tMin = tMin.Add(-pad)
		tMax = tMax.Add(pad)
	}

	// Determine the temperature domain. Default 36.5–40.0, expand to include
	// any out-of-range readings with a 0.2 °C margin.
	yMin, yMax := 36.5, 40.0
	for _, t := range temps {
		if t.ValueC < yMin+0.01 {
			yMin = math.Floor((t.ValueC-0.2)*10) / 10
		}
		if t.ValueC > yMax-0.01 {
			yMax = math.Ceil((t.ValueC+0.2)*10) / 10
		}
	}
	if yMax-yMin < 1.0 {
		yMax = yMin + 1.0
	}

	// Plot box in viewBox units.
	x1 := float64(chartLeftPx)
	y1 := float64(chartTopPx)
	x2 := float64(chartW - chartRightPx)
	y2 := float64(chartH - chartBottomP)
	chart.Plot.X1, chart.Plot.Y1, chart.Plot.X2, chart.Plot.Y2 = x1, y1, x2, y2

	tRange := tMax.Sub(tMin)
	if tRange <= 0 {
		tRange = time.Minute
	}
	mapX := func(t time.Time) float64 {
		frac := float64(t.Sub(tMin)) / float64(tRange)
		return x1 + frac*(x2-x1)
	}
	mapY := func(v float64) float64 {
		frac := (v - yMin) / (yMax - yMin)
		return y2 - frac*(y2-y1)
	}

	// Y ticks every 0.5 °C, anchored to a multiple of 0.5 at or below yMin.
	yStart := math.Floor(yMin*2) / 2
	for v := yStart; v <= yMax+1e-9; v += 0.5 {
		if v < yMin-1e-9 {
			continue
		}
		chart.YTicks = append(chart.YTicks, ChartYTick{
			Y:      mapY(v),
			ValueC: v,
			Label:  fmt.Sprintf("%.1f°", v),
		})
	}

	// X ticks: choose a stride that yields 4–7 labels.
	chart.XTicks = buildTimeTicks(tMin, tMax, mapX)

	// Temperature points + polyline.
	sortedTemps := append([]models.TemperatureEvent(nil), temps...)
	sort.Slice(sortedTemps, func(i, j int) bool { return sortedTemps[i].At.Before(sortedTemps[j].At) })
	var poly string
	for i, t := range sortedTemps {
		px := mapX(t.At)
		py := mapY(t.ValueC)
		chart.Points = append(chart.Points, ChartPoint{
			X:             px,
			Y:             py,
			ValueC:        t.ValueC,
			TooltipPrefix: fmt.Sprintf("%.1f °C — ", t.ValueC),
			TooltipTS:     isoTS(t.At),
		})
		if i > 0 {
			poly += " "
		}
		poly += fmt.Sprintf("%.2f,%.2f", px, py)
	}
	chart.PolylinePts = poly

	// Markers for dose events.
	for _, d := range doses {
		dx := mapX(d.at)
		label := d.medName
		if d.deleted {
			label += " (deleted)"
		}
		chart.Markers = append(chart.Markers, ChartMarker{
			X:             dx,
			YTop:          y1,
			YBottom:       y2,
			Color:         d.color,
			TooltipPrefix: fmt.Sprintf("%s — ", label),
			TooltipTS:     isoTS(d.at),
		})
	}

	// Legend. Temperature first (when there are readings), then the
	// medications that actually have a dose in the window.
	if len(temps) > 0 {
		chart.Legend = append(chart.Legend, ChartLegend{Name: "Temperature", Color: "#dc2626"})
	}
	used := map[string]bool{}
	for _, d := range doses {
		used[d.medID] = true
	}
	for _, id := range medIDs {
		if used[id] {
			chart.Legend = append(chart.Legend, ChartLegend{Name: nameOf[id], Color: colorOf[id]})
		}
	}
	return chart
}

// buildTimeTicks selects ~5 evenly-spaced ticks across [tMin, tMax] with
// labels that switch between time-of-day and date depending on range.
func buildTimeTicks(tMin, tMax time.Time, mapX func(time.Time) float64) []ChartXTick {
	span := tMax.Sub(tMin)
	// Pick a number of ticks.
	n := 5
	step := time.Duration(int64(span) / int64(n))

	// Snap step to a "nice" rounded unit so labels read nicely.
	candidates := []time.Duration{
		1 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
		1 * time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
		24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour,
	}
	for _, c := range candidates {
		if c >= step {
			step = c
			break
		}
	}

	// Choose client-side label format hint.
	fmtHint := "HH:MM"
	goFmt := "15:04"
	if span > 36*time.Hour {
		fmtHint = "MM-DD"
		goFmt = "01-02"
	}
	if span > 60*24*time.Hour {
		fmtHint = "YYYY-MM"
		goFmt = "2006-01"
	}

	var ticks []ChartXTick
	// Align the first tick to the floor of step from tMin.
	startUnix := (tMin.Unix() / int64(step.Seconds())) * int64(step.Seconds())
	t := time.Unix(startUnix, 0)
	if t.Before(tMin) {
		t = t.Add(step)
	}
	for ; !t.After(tMax); t = t.Add(step) {
		ticks = append(ticks, ChartXTick{
			X:     mapX(t),
			Label: t.UTC().Format(goFmt), // fallback if JS is disabled
			TS:    isoTS(t),
			Fmt:   fmtHint,
		})
	}
	if len(ticks) == 0 {
		ticks = append(ticks, ChartXTick{
			X:     mapX(tMin),
			Label: tMin.UTC().Format(goFmt),
			TS:    isoTS(tMin),
			Fmt:   fmtHint,
		})
	}
	return ticks
}
