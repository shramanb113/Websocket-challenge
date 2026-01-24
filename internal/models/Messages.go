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
	RoomID    string      `json:"room_id"`
	Sender    string      `json:"sender"`
	Content   string      `json:"content"`
	Type      MessageType `json:"type"`
	CreatedAt time.Time   `json:"createdAt"`
}
