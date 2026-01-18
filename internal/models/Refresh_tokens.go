package models

import (
	"net"
	"time"

	"github.com/google/uuid"
)

type RefreshToken struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`

	TokenHashed string `json:"-"`

	UserAgent string    `json:"user_agent"`
	ClientIP  net.IP    `json:"client_ip"`
	IsRevoked bool      `json:"is_revoked"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
