package handlers

import (
	"context"
	"net/http"

	"github.com/lithammer/shortuuid/v4"

	"medtrack/internal/store"
)

type contextKey string

const storeContextKey contextKey = "diaryStore"

// SessionMiddleware assigns a per-session DiaryStore to each request via context.
func SessionMiddleware(sm *store.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionID := ""
			cookie, err := r.Cookie("session_id")
			if err == nil {
				sessionID = cookie.Value
			}
			if sessionID == "" {
				sessionID = shortuuid.New()
				http.SetCookie(w, &http.Cookie{
					Name:     "session_id",
					Value:    sessionID,
					Path:     "/",
					MaxAge:   7 * 24 * 60 * 60,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
			}
			s := sm.GetOrCreateStore(sessionID)
			ctx := context.WithValue(r.Context(), storeContextKey, s)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func storeFromContext(r *http.Request) *store.DiaryStore {
	return r.Context().Value(storeContextKey).(*store.DiaryStore)
}
