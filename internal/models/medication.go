package models

import (
	"strconv"
	"time"
)

// RangeInt represents an optional scalar-or-range integer.
// Min == Max == 0 means "not configured".
// Min == Max != 0 is a scalar.
type RangeInt struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

func (r RangeInt) IsZero() bool   { return r.Min == 0 && r.Max == 0 }
func (r RangeInt) IsScalar() bool { return r.Min == r.Max }

// IntervalHours is the recommended wait between consecutive doses.
// MinHours == X (don't take earlier than this since last dose).
// MaxHours == Y (don't take later than this since last dose).
type IntervalHours struct {
	MinHours float64 `json:"minHours"`
	MaxHours float64 `json:"maxHours"`
}

func (i IntervalHours) IsZero() bool { return i.MinHours == 0 && i.MaxHours == 0 }

// CycleDuration is the length of one cycle expressed as (value, unit).
// Default in the form: value=1, unit="day".
type CycleDuration struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

func (c CycleDuration) IsZero() bool { return c.Value == 0 }

// Hours returns the cycle length in hours; an unset cycle defaults to 24h.
func (c CycleDuration) Hours() float64 {
	if c.IsZero() {
		return 24
	}
	switch c.Unit {
	case "hour":
		return c.Value
	case "week":
		return c.Value * 24 * 7
	case "day":
		fallthrough
	default:
		return c.Value * 24
	}
}

type EventType string

const (
	EventDose         EventType = "dose"
	EventDoseRevert   EventType = "dose_revert"
	EventCycleAdvance EventType = "cycle_advance"
	EventCycleRevert  EventType = "cycle_revert"
)

// Event is one entry in a medication's append-only audit log.
type Event struct {
	Type       EventType  `json:"type"`
	At         time.Time  `json:"at"`
	CycleIndex int        `json:"cycleIndex"`
	TakingAt   *time.Time `json:"takingAt,omitempty"`
}

// Medication holds the configuration and live counters for one medication.
type Medication struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	PerCycle       RangeInt               `json:"perCycle,omitempty"`
	CycleDuration  CycleDuration          `json:"cycleDuration,omitempty"`
	CyclesTotal    RangeInt               `json:"cyclesTotal,omitempty"`
	Interval       IntervalHours          `json:"interval,omitempty"`
	CycleIndex     int                    `json:"cycleIndex"`
	TakingsByCycle map[string][]time.Time `json:"takingsByCycle"`
	Events         []Event                `json:"events"`
}

func (m *Medication) cycleKey(idx int) string { return strconv.Itoa(idx) }

// TakingsForCycle returns the (sorted) list of takings for the given cycle.
func (m *Medication) TakingsForCycle(idx int) []time.Time {
	if m.TakingsByCycle == nil {
		return nil
	}
	return m.TakingsByCycle[m.cycleKey(idx)]
}

// TakingsForCurrentCycle is shorthand for TakingsForCycle(m.CycleIndex).
func (m *Medication) TakingsForCurrentCycle() []time.Time {
	return m.TakingsForCycle(m.CycleIndex)
}

// LastTakingAny returns the latest taking across all cycles, or zero time if none.
func (m *Medication) LastTakingAny() time.Time {
	var latest time.Time
	for _, ts := range m.TakingsByCycle {
		for _, t := range ts {
			if t.After(latest) {
				latest = t
			}
		}
	}
	return latest
}

// AppendTaking adds a taking to the current cycle and returns it.
func (m *Medication) AppendTaking(at time.Time) {
	if m.TakingsByCycle == nil {
		m.TakingsByCycle = map[string][]time.Time{}
	}
	key := m.cycleKey(m.CycleIndex)
	m.TakingsByCycle[key] = append(m.TakingsByCycle[key], at)
}

// PopTaking removes and returns the last taking in the current cycle.
// Returns zero time and false when there's nothing to pop.
func (m *Medication) PopTaking() (time.Time, bool) {
	if m.TakingsByCycle == nil {
		return time.Time{}, false
	}
	key := m.cycleKey(m.CycleIndex)
	cur := m.TakingsByCycle[key]
	if len(cur) == 0 {
		return time.Time{}, false
	}
	last := cur[len(cur)-1]
	m.TakingsByCycle[key] = cur[:len(cur)-1]
	if len(m.TakingsByCycle[key]) == 0 {
		delete(m.TakingsByCycle, key)
	}
	return last, true
}

// TemperatureEvent is one body-temperature reading.
type TemperatureEvent struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	ValueC float64   `json:"valueC"`
}

// Diary is the top-level container holding all medications.
// DeletedMedications keeps the audit trail for medications the user removed.
type Diary struct {
	Name               string             `json:"name"`
	Medications        []Medication       `json:"medications"`
	DeletedMedications []Medication       `json:"deletedMedications,omitempty"`
	Temperatures       []TemperatureEvent `json:"temperatures,omitempty"`
}
