package models

import (
	"time"

	"github.com/google/uuid"
)

type MessageType string

const (
	TypeChat     MessageType = "chat"
	TypePrivate  MessageType = "private"
	TypeSystem   MessageType = "system"
	TypeUserList MessageType = "user_list"
)

type Message struct {
	ID        uuid.UUID   `json:"id"`
	RoomID    string      `json:"roomID"`
	Sender    string      `json:"sender"`
	Content   string      `json:"content"`
	Type      MessageType `json:"type"`
	Target    string      `json:"target,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}
