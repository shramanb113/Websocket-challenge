package middleware

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"websocket-challenge/internal/auth"
	"websocket-challenge/internal/repository"

	"github.com/jackc/pgx/v5"
)

type contextKey string

const UserIDKey contextKey = "user_id"

func getIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
		return host
	}
	return host
}

func Authenticate(repo repository.UserRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			currentIP := getIP(r)
			currentUserAgent := r.UserAgent()

			cookie, err := r.Cookie("access_token")
			if err != nil {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			claims, err := auth.ValidateToken(cookie.Value)
			if err != nil {
				log.Printf("[AUTH] Invalid token from %s: %v", currentIP, err)
				http.Error(w, "Session expired or invalid", http.StatusUnauthorized)
				return
			}

			expectedFingerprint := auth.GenerateFingerprint(currentIP, currentUserAgent)
			if claims.Fingerprint != expectedFingerprint {
				log.Printf("[SECURITY ALERT] Fingerprint mismatch! User: %s, Request IP: %s, Cookie IP: %s",
					claims.UserID, currentIP, r.RemoteAddr)
				http.Error(w, "Security context violation", http.StatusForbidden)
				return
			}

			dbCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			user, err := repo.GetUserByID(dbCtx, claims.UserID)

			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					log.Printf("[AUTH] DB Timeout during auth check for user %s", claims.UserID)
					http.Error(w, "Service temporary unavailable", http.StatusServiceUnavailable)
					return
				}
				if errors.Is(err, pgx.ErrNoRows) {
					log.Printf("[AUTH] Token valid but user no longer exists: %s", claims.UserID)
					http.Error(w, "User account not found", http.StatusUnauthorized)
					return
				}
				log.Printf("[ERROR] Middleware DB lookup failed: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			if user.IsBanned {
				http.Error(w, "Account disabled", http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
