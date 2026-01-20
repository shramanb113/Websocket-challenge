package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
	"websocket-challenge/internal/config"
	"websocket-challenge/internal/models"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	jwtKeyCache []byte
	once        sync.Once
)

type CustomClaims struct {
	UserID      uuid.UUID `json:"uid"`
	Fingerprint string    `json:"fpt"`
	jwt.RegisteredClaims
}

func getJwtKey() []byte {

	once.Do(func() {
		cfg := config.Load()
		if cfg.AuthKey == "" {
			log.Printf("[AUTH] CRITICAL: AuthKey is empty in config! Tokens will be insecure.")
		}
		jwtKeyCache = []byte(cfg.AuthKey)
		log.Println("[AUTH] Configuration loaded and JWT key cached.")
	})
	return jwtKeyCache
}

func GenerateToken(userId uuid.UUID, user_agent string, ipAddress string) (string, error) {
	now := time.Now()
	expiresAt := now.Add(15 * time.Minute)

	claims := &CustomClaims{
		UserID:      userId,
		Fingerprint: GenerateFingerprint(ipAddress, user_agent),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "GoHub",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-2 * time.Minute)),
			Subject:   userId.String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJwtKey())
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

func GenerateRandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GenerateFingerprint(ip, ua string) string {
	combined := fmt.Sprintf("%s|%s", ip, ua)
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

func CreateRefreshToken(userId uuid.UUID, ip string, userAgent string) (string, *models.RefreshToken, error) {
	rawToken, err := GenerateRandomString(32)
	if err != nil {
		return "", nil, err
	}

	h := sha256.New()
	h.Write([]byte(rawToken))
	hashedToken := hex.EncodeToString(h.Sum(nil))

	tokenModel := &models.RefreshToken{
		UserID:      userId,
		TokenHashed: hashedToken,
		UserAgent:   userAgent,
		ClientIP:    net.ParseIP(ip),
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour),
	}

	return rawToken, tokenModel, nil

}
