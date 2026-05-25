package store

import (
	"strings"
	"sync"
	"time"

	"github.com/lithammer/shortuuid/v4"

	"medtrack/internal/models"
)

type DiaryStore struct {
	mu    sync.RWMutex
	diary models.Diary
}

func NewDiaryStore() *DiaryStore {
	return &DiaryStore{
		diary: models.Diary{
			Name:        "My medications",
			Medications: []models.Medication{},
		},
	}
}

// Snapshot returns a deep copy of the diary safe to mutate by the caller.
func (s *DiaryStore) Snapshot() models.Diary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyDiary(s.diary)
}

// Replace overwrites the diary, e.g. for import or clear.
func (s *DiaryStore) Replace(d models.Diary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diary = deepCopyDiary(d)
	if s.diary.Medications == nil {
		s.diary.Medications = []models.Medication{}
	}
}

// Name returns the diary's display name.
func (s *DiaryStore) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.diary.Name
}

// SetName updates the diary title.
func (s *DiaryStore) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diary.Name = strings.TrimSpace(name)
}

// Create adds a new medication, assigning a new ID.
func (s *DiaryStore) Create(m models.Medication) models.Medication {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.ID = shortuuid.New()
	if m.TakingsByCycle == nil {
		m.TakingsByCycle = map[string][]time.Time{}
	}
	if m.Events == nil {
		m.Events = []models.Event{}
	}
	s.diary.Medications = append(s.diary.Medications, m)
	return deepCopyMed(m)
}

// Update overwrites configuration fields for the medication with the given ID.
// Live state (CycleIndex, TakingsByCycle, Events) is preserved.
func (s *DiaryStore) Update(id string, m models.Medication) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.diary.Medications {
		if s.diary.Medications[i].ID == id {
			existing := &s.diary.Medications[i]
			existing.Name = m.Name
			existing.PerCycle = m.PerCycle
			existing.CycleDuration = m.CycleDuration
			existing.CyclesTotal = m.CyclesTotal
			existing.Interval = m.Interval
			return true
		}
	}
	return false
}

// Delete removes the medication with the given ID from the active list and
// moves it into DeletedMedications so its event log survives.
func (s *DiaryStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.diary.Medications {
		if s.diary.Medications[i].ID == id {
			removed := s.diary.Medications[i]
			s.diary.Medications = append(s.diary.Medications[:i], s.diary.Medications[i+1:]...)
			s.diary.DeletedMedications = append(s.diary.DeletedMedications, removed)
			return true
		}
	}
	return false
}

// DeletedMedications returns a deep copy of the soft-deleted medications,
// for use by the event log.
func (s *DiaryStore) DeletedMedications() []models.Medication {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.Medication, len(s.diary.DeletedMedications))
	for i := range s.diary.DeletedMedications {
		out[i] = deepCopyMed(s.diary.DeletedMedications[i])
	}
	return out
}

func (s *DiaryStore) findIndex(id string) int {
	for i := range s.diary.Medications {
		if s.diary.Medications[i].ID == id {
			return i
		}
	}
	return -1
}

// RecordDose appends a taking to the current cycle and logs a dose event.
func (s *DiaryStore) RecordDose(id string, now time.Time) (models.Medication, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return models.Medication{}, false
	}
	m := &s.diary.Medications[idx]
	m.AppendTaking(now)
	taking := now
	m.Events = append(m.Events, models.Event{
		Type:       models.EventDose,
		At:         now,
		CycleIndex: m.CycleIndex,
		TakingAt:   &taking,
	})
	return deepCopyMed(*m), true
}

// RevertDose pops the last taking in the current cycle and logs a revert event.
// Returns the (now-updated) medication. If there was nothing to pop, no event
// is logged and the medication is returned unchanged.
func (s *DiaryStore) RevertDose(id string, now time.Time) (models.Medication, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return models.Medication{}, false
	}
	m := &s.diary.Medications[idx]
	popped, ok := m.PopTaking()
	if !ok {
		return deepCopyMed(*m), true
	}
	m.Events = append(m.Events, models.Event{
		Type:       models.EventDoseRevert,
		At:         now,
		CycleIndex: m.CycleIndex,
		TakingAt:   &popped,
	})
	return deepCopyMed(*m), true
}

// AdvanceCycle bumps CycleIndex by one and logs a cycle_advance event.
func (s *DiaryStore) AdvanceCycle(id string, now time.Time) (models.Medication, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return models.Medication{}, false
	}
	m := &s.diary.Medications[idx]
	m.CycleIndex++
	m.Events = append(m.Events, models.Event{
		Type:       models.EventCycleAdvance,
		At:         now,
		CycleIndex: m.CycleIndex,
	})
	return deepCopyMed(*m), true
}

// RevertCycle decrements CycleIndex (clamped at 0) and logs a cycle_revert event.
func (s *DiaryStore) RevertCycle(id string, now time.Time) (models.Medication, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return models.Medication{}, false
	}
	m := &s.diary.Medications[idx]
	if m.CycleIndex == 0 {
		return deepCopyMed(*m), true
	}
	m.CycleIndex--
	m.Events = append(m.Events, models.Event{
		Type:       models.EventCycleRevert,
		At:         now,
		CycleIndex: m.CycleIndex,
	})
	return deepCopyMed(*m), true
}

// ResetProgress clears every medication's live state, keeping configuration.
// Soft-deleted medications are also wiped — they exist only for their history.
func (s *DiaryStore) ResetProgress() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.diary.Medications {
		m := &s.diary.Medications[i]
		m.CycleIndex = 0
		m.TakingsByCycle = map[string][]time.Time{}
		m.Events = []models.Event{}
	}
	s.diary.DeletedMedications = nil
}

// Clear wipes the diary entirely.
func (s *DiaryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diary = models.Diary{
		Name:        "My medications",
		Medications: []models.Medication{},
	}
}

func deepCopyDiary(d models.Diary) models.Diary {
	out := models.Diary{Name: d.Name}
	out.Medications = make([]models.Medication, len(d.Medications))
	for i := range d.Medications {
		out.Medications[i] = deepCopyMed(d.Medications[i])
	}
	if len(d.DeletedMedications) > 0 {
		out.DeletedMedications = make([]models.Medication, len(d.DeletedMedications))
		for i := range d.DeletedMedications {
			out.DeletedMedications[i] = deepCopyMed(d.DeletedMedications[i])
		}
	}
	return out
}

func deepCopyMed(m models.Medication) models.Medication {
	out := m
	if m.TakingsByCycle != nil {
		out.TakingsByCycle = make(map[string][]time.Time, len(m.TakingsByCycle))
		for k, v := range m.TakingsByCycle {
			cp := make([]time.Time, len(v))
			copy(cp, v)
			out.TakingsByCycle[k] = cp
		}
	} else {
		out.TakingsByCycle = map[string][]time.Time{}
	}
	if m.Events != nil {
		out.Events = make([]models.Event, len(m.Events))
		copy(out.Events, m.Events)
	} else {
		out.Events = []models.Event{}
	}
	return out
}
