package types

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
	TypeAck      MessageType = "user_ack"
	TypeTyping   MessageType = "user_typing"
	TypeKick     MessageType = "user_kick"
)

type MessageStatus int

const (
	StatusSaved MessageStatus = iota
	StatusDelivered
	StatusSeen
)

type Message struct {
	ID        uuid.UUID     `json:"id"`
	RoomID    string        `json:"roomID"`
	Sender    string        `json:"sender"`
	Content   string        `json:"content"`
	Type      MessageType   `json:"type"`
	Target    string        `json:"target,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Status    MessageStatus `json:"status"`

	SenderServerID string `json:"sender_server_id"`
	FromRedis      bool   `json:"-"`
}
