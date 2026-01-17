package auth

import (
	"errors"
	"fmt"
	"log"
	"time"
	"websocket-challenge/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type CustomClaims struct {
	UserID uuid.UUID `json:"user_id"`
	jwt.RegisteredClaims
}

func getJwtKey() []byte {
	cfg := config.Load()
	if cfg.AuthKey == "" {
		log.Printf("[AUTH] WARNING: AuthKey is empty in config!")
	}
	return []byte(cfg.AuthKey)
}

func GenerateToken(userId uuid.UUID) (string, error) {
	expiresAt := time.Now().Add(24 * time.Hour)
	log.Printf("[AUTH] Generating token for UserID: %s (Expires: %s)", userId, expiresAt.Format(time.RFC3339))

	claims := &CustomClaims{
		UserID: userId,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "GoHub",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(getJwtKey())
	if err != nil {
		log.Printf("[AUTH] ERROR: Failed to sign token for user %s: %v", userId, err)
		return "", err
	}

	log.Printf("[AUTH] Token successfully generated for UserID: %s", userId)
	return tokenString, nil
}

func ValidateToken(tokenString string) (*CustomClaims, error) {
	log.Printf("[AUTH] Attempting to validate token...")

	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(t *jwt.Token) (any, error) {
		log.Printf("[AUTH] Token header algorithm: %v", t.Header["alg"])

		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			errDetail := fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			log.Printf("[AUTH] VALIDATION FAILED: %v", errDetail)
			return nil, errDetail
		}
		return getJwtKey(), nil
	})

	if err != nil {
		log.Printf("[AUTH] JWT Parse Error: %v", err)
		return nil, err
	}

	if claims, ok := token.Claims.(*CustomClaims); ok && token.Valid {
		log.Printf("[AUTH] SUCCESS: Token valid for UserID: %s", claims.UserID)
		return claims, nil
	}

	log.Printf("[AUTH] VALIDATION FAILED: Token claims invalid or token not valid")
	return nil, errors.New("invalid token")
}
