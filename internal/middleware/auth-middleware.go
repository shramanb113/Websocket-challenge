package middleware

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
	"websocket-challenge/internal/auth"
	"websocket-challenge/internal/repository"
)

type contextKey string

const UserIDKey contextKey = "userID"

func Authenticate(repo *repository.PostgresUserRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("token")
			if err != nil {
				if errors.Is(err, http.ErrNoCookie) {
					log.Println("[AUTH] No token cookie found")
					http.Error(w, "Authentication required", http.StatusUnauthorized)
					return
				}
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}

			tokenStr := cookie.Value

			claims, err := auth.ValidateToken(tokenStr)
			if err != nil {
				log.Printf("[AUTH] Token validation failed: %v", err)
				http.Error(w, "Invalid or malformed token", http.StatusUnauthorized)
				return
			}

			if claims.ExpiresAt != nil && time.Now().After(claims.ExpiresAt.Time) {
				log.Printf("[AUTH] Token expired for UserID: %s", claims.UserID)
				http.Error(w, "Token expired", http.StatusUnauthorized)
				return
			}

			user, err := repo.GetUserByID(r.Context(), claims.UserID)
			if err != nil {
				log.Printf("[AUTH] User in token does not exist in DB: %s", claims.UserID)
				http.Error(w, "User no longer exists", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, user)

			log.Printf("[AUTH] User %s authenticated successfully", user.Username)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
