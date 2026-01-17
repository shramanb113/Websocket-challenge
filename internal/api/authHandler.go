package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"websocket-challenge/internal/auth"
	"websocket-challenge/internal/models"
	"websocket-challenge/internal/repository"
	"websocket-challenge/internal/types"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Helper to validate email format
func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

func LoginHandler(repo *repository.PostgresUserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload types.LoginRequest

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Printf("[LOGIN] Decode error: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Edge Case: Empty strings
		payload.Username = strings.TrimSpace(payload.Username)
		if payload.Username == "" || payload.Password == "" {
			log.Println("[LOGIN] Attempt with empty username or password")
			http.Error(w, "Username and password are required", http.StatusBadRequest)
			return
		}

		user, err := repo.GetUserByUsername(r.Context(), payload.Username)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				log.Printf("[LOGIN] User not found: %s", payload.Username)
				http.Error(w, "Invalid username or password", http.StatusUnauthorized)
				return
			}
			log.Printf("[LOGIN] Database error for %s: %v", payload.Username, err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if !auth.VerifyPassword(payload.Password, user.Password_Hash) {
			log.Printf("[LOGIN] Invalid password for user: %s", payload.Username)
			http.Error(w, "Invalid username or password", http.StatusUnauthorized)
			return
		}

		token, err := auth.GenerateToken(user.ID)
		if err != nil {
			log.Printf("[LOGIN] Token generation failed for %s: %v", user.ID, err)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "token",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(24 * time.Hour),
			HttpOnly: true,
			Secure:   false, // Set true in production
			SameSite: http.SameSiteLaxMode,
		})

		log.Printf("[LOGIN] Success: User %s logged in", user.Username)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.UserDTO{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
		})
	}
}

func SignupHandler(repo *repository.PostgresUserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload types.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Printf("[SIGNUP] Decode error: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		payload.Username = strings.TrimSpace(payload.Username)
		payload.Email = strings.TrimSpace(strings.ToLower(payload.Email))

		if payload.Username == "" || payload.Email == "" || payload.Password == "" {
			log.Println("[SIGNUP] Missing fields in request")
			http.Error(w, "All fields (username, email, password) are required", http.StatusBadRequest)
			return
		}

		if !isValidEmail(payload.Email) {
			log.Printf("[SIGNUP] Invalid email format: %s", payload.Email)
			http.Error(w, "Invalid email format", http.StatusBadRequest)
			return
		}

		if len(payload.Password) < 8 {
			http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		if _, err := repo.GetUserByUsername(r.Context(), payload.Username); err == nil {
			log.Printf("[SIGNUP] Conflict: Username %s already exists", payload.Username)
			http.Error(w, "Username already taken", http.StatusConflict)
			return
		}

		hashed, err := auth.HashPassword(payload.Password)
		if err != nil {
			log.Printf("[SIGNUP] Hashing error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		user := &models.User{
			ID:            uuid.New(),
			Username:      payload.Username,
			Email:         payload.Email,
			Password_Hash: hashed,
			CreatedAt:     time.Now(),
		}

		if err := repo.CreateUser(r.Context(), user); err != nil {
			log.Printf("[SIGNUP] DB Create error for %s: %v", payload.Username, err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}

		token, err := auth.GenerateToken(user.ID)
		if err != nil {
			log.Printf("[SIGNUP] Token generation failed: %v", err)
			http.Error(w, "User created, but failed to start session. Please login.", http.StatusCreated)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "token",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(24 * time.Hour),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		log.Printf("[SIGNUP] Success: New user created: %s", user.Username)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(types.UserDTO{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
		})
	}
}

func Logouthandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     "token",
			Value:    "",
			Expires:  time.Unix(0, 0),
			HttpOnly: true,
			MaxAge:   -1,
			SameSite: http.SameSiteLaxMode,
		})

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Logged Out Successfully"))
	}
}
