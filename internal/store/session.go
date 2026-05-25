package store

import (
	"sync"
	"time"
)

type sessionEntry struct {
	store      *DiaryStore
	lastAccess time.Time
}

// SessionManager manages per-session DiaryStore instances.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*sessionEntry
}

func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*sessionEntry),
	}
	go sm.cleanupLoop()
	return sm
}

func (sm *SessionManager) GetOrCreateStore(sessionID string) *DiaryStore {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entry, ok := sm.sessions[sessionID]
	if !ok {
		entry = &sessionEntry{store: NewDiaryStore()}
		sm.sessions[sessionID] = entry
	}
	entry.lastAccess = time.Now()
	return entry.store
}

func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		sm.cleanup(24 * time.Hour)
	}
}

func (sm *SessionManager) cleanup(maxAge time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, entry := range sm.sessions {
		if entry.lastAccess.Before(cutoff) {
			delete(sm.sessions, id)
		}
	}
}
