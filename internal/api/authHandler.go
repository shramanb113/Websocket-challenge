package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
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
	"github.com/jackc/pgx/v5/pgconn"
)

// Helper to validate email format
func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

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

func LoginHandler(repoUser repository.UserRepository, repoRefreshToken *repository.PostgresRefreshTokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload types.LoginRequest

		userAgent := r.UserAgent()
		ip := getIP(r)

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Printf("[LOGIN] Decode error: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		payload.Username = strings.TrimSpace(payload.Username)
		if payload.Username == "" || payload.Password == "" {
			log.Println("[LOGIN] Attempt with empty username or password")
			http.Error(w, "Username and password are required", http.StatusBadRequest)
			return
		}

		user, err := repoUser.GetUserByUsername(r.Context(), payload.Username)
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

		token, err := auth.GenerateToken(user.ID, userAgent, ip)
		if err != nil {
			log.Printf("[LOGIN] Access Token generation failed for %s: %v", user.ID, err)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		refreshToken, refreshTokenModel, err := auth.CreateRefreshToken(user.ID, userAgent, ip)

		if err != nil {
			log.Printf("[LOGIN] Refresh Token generation failed for %s: %v", user.ID, err)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		err = repoRefreshToken.SaveRefreshToken(r.Context(), refreshTokenModel)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				log.Printf("[LOGIN] Postgres Error: Code %s, Message: %s", pgErr.Code, pgErr.Message)
			} else {
				log.Printf("[LOGIN] Unknown Database Error: %v", err)
			}

			http.Error(w, "Failed to initialize session", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "access_token",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(15 * time.Minute),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		})

		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    refreshToken,
			Path:     "/api/auth/refresh",
			Expires:  time.Now().Add(7 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
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

func SignupHandler(repoUser repository.UserRepository, repoRefreshToken *repository.PostgresRefreshTokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload types.RegisterRequest

		userAgent := r.UserAgent()
		ip := getIP(r)

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

		if _, err := repoUser.GetUserByUsername(r.Context(), payload.Username); err == nil {
			log.Printf("[SIGNUP] Conflict: Username %s already exists", payload.Username)
			http.Error(w, "Username already taken", http.StatusConflict)
			return
		}
		if _, err := repoUser.GetUserByEmail(r.Context(), payload.Email); err == nil {
			log.Printf("[SIGNUP] Conflict: Email %s already exists", payload.Email)
			http.Error(w, "Email already exists", http.StatusConflict)
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

		if err := repoUser.CreateUser(r.Context(), user); err != nil {
			log.Printf("[SIGNUP] DB Create error for %s: %v", payload.Username, err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}

		token, err := auth.GenerateToken(user.ID, r.UserAgent(), getIP(r))
		if err != nil {
			log.Printf("[SIGNUP] Token generation failed: %v", err)
			http.Error(w, "User created, but failed to start session. Please login.", http.StatusCreated)
			return
		}

		refreshToken, refreshTokenModel, err := auth.CreateRefreshToken(user.ID, userAgent, ip)
		if err != nil {
			log.Printf("[LOGIN] Refresh Token generation failed for %s: %v", user.ID, err)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		err = repoRefreshToken.SaveRefreshToken(r.Context(), refreshTokenModel)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				log.Printf("[LOGIN] Postgres Error: Code %s, Message: %s", pgErr.Code, pgErr.Message)
			} else {
				log.Printf("[LOGIN] Unknown Database Error: %v", err)
			}

			http.Error(w, "Failed to initialize session", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "access_token",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(15 * time.Minute),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		})

		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    refreshToken,
			Path:     "/api/auth/refresh",
			Expires:  time.Now().Add(7 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
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

func Logouthandler(repoRefreshToken *repository.PostgresRefreshTokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("refresh_token")

		if err == nil {
			valBytes := sha256.Sum256([]byte(cookie.Value))
			tokenhashed := hex.EncodeToString(valBytes[:])

			token, err := repoRefreshToken.GetTokenByHash(r.Context(), tokenhashed)
			if err == nil {
				_ = repoRefreshToken.RevokeToken(r.Context(), token.ID)
			}
		}
		past := time.Unix(0, 0)

		http.SetCookie(w, &http.Cookie{
			Name:  "access_token",
			Value: "", Path: "/",
			Expires:  past,
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		})

		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    "",
			Path:     "/api/auth/refresh",
			Expires:  past,
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		})

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Logged Out Successfully"))
	}
}

func RefreshHandler(repoRefreshToken *repository.PostgresRefreshTokenRepo, repouser repository.UserRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userAgent := r.UserAgent()
		ipStr := getIP(r)
		currentIP := net.ParseIP(ipStr)

		cookie, err := r.Cookie("refresh_token")
		if err != nil {
			log.Printf("[AUTH] Refresh attempt failed: Missing cookie (IP: %s)", ipStr)
			http.Error(w, "Refresh token required", http.StatusUnauthorized)
			return
		}

		h := sha256.Sum256([]byte(cookie.Value))
		tokenHashed := hex.EncodeToString(h[:])

		tokenModel, err := repoRefreshToken.GetTokenByHash(r.Context(), tokenHashed)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				log.Printf("[SECURITY] Potential Token Reuse or Invalid Token: %s (IP: %s)", tokenHashed[:8], ipStr)
				http.Error(w, "Invalid session", http.StatusUnauthorized)
				return
			}
			log.Printf("[ERROR] Database failure during refresh lookup: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if time.Now().After(tokenModel.ExpiresAt) {
			log.Printf("[AUTH] Session expired for User: %s", tokenModel.UserID)
			http.Error(w, "Session expired", http.StatusUnauthorized)
			return
		}

		if tokenModel.UserAgent != userAgent || !tokenModel.ClientIP.Equal(currentIP) {
			log.Printf("[SECURITY ALERT] Context mismatch for User %s. Expected IP: %s, Got: %s",
				tokenModel.UserID, tokenModel.ClientIP, ipStr)

			http.Error(w, "Security context mismatch", http.StatusUnauthorized)
			return
		}

		if err := repoRefreshToken.RevokeToken(r.Context(), tokenModel.ID); err != nil {
			log.Printf("[ERROR] Failed to rotate token %s: %v", tokenModel.ID, err)
			http.Error(w, "Could not refresh session", http.StatusInternalServerError)
			return
		}

		accessToken, err := auth.GenerateToken(tokenModel.UserID, userAgent, ipStr)
		if err != nil {
			log.Printf("[ERROR] JWT Generation error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		rawRefreshToken, newRefreshModel, err := auth.CreateRefreshToken(tokenModel.UserID, userAgent, ipStr)
		if err != nil {
			log.Printf("[ERROR] Refresh string generation error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if err := repoRefreshToken.SaveRefreshToken(r.Context(), newRefreshModel); err != nil {
			log.Printf("[ERROR] Failed to save new refresh token for user %s: %v", tokenModel.UserID, err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "access_token",
			Value:    accessToken,
			Path:     "/",
			Expires:  time.Now().Add(15 * time.Minute),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		})

		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    rawRefreshToken,
			Path:     "/api/auth/refresh",
			Expires:  time.Now().Add(7 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		})

		log.Printf("[AUTH] Session rotated successfully for User: %s", tokenModel.UserID)
		w.WriteHeader(http.StatusOK)
	}
}
