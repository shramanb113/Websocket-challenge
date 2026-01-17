package chat

import (
	"encoding/json"
	"log"
	"time"

	"websocket-challenge/internal/middleware"

	"github.com/gorilla/websocket"
)

type MessageType string

const (
	TypeChat     MessageType = "chat"
	TypePrivate  MessageType = "private"
	TypeSystem   MessageType = "system"
	TypeUserList MessageType = "user_list"
)

type Message struct {
	Sender    string      `json:"sender"`
	Content   string      `json:"content"`
	Type      MessageType `json:"type"`
	Target    string      `json:"target,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

type Hub struct {
	Clients    map[string]*Client
	History    []Message
	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan *Message
	Quit       chan struct{}
}

type Client struct {
	Conn    *websocket.Conn
	Name    string
	Send    chan []byte
	Hub     *Hub
	Limiter *middleware.RateLimiter
}

func NewHub() *Hub {
	log.Println("[HUB] Initializing new Hub instance...")
	return &Hub{
		Clients:    make(map[string]*Client),
		History:    make([]Message, 0),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Broadcast:  make(chan *Message, 256),
		Quit:       make(chan struct{}),
	}
}

func (h *Hub) getConnectedUsers() []string {
	users := make([]string, 0, len(h.Clients))
	for name := range h.Clients {
		users = append(users, name)
	}
	return users
}

func (h *Hub) broadcastUserList() {
	log.Println("[HUB] Generating fresh user list for broadcast...")
	users := h.getConnectedUsers()
	rawList, _ := json.Marshal(users)

	message := &Message{
		Sender:    "SYSTEM",
		Content:   string(rawList),
		Type:      TypeUserList,
		Timestamp: time.Now().Unix(),
	}

	select {
	case h.Broadcast <- message:
		log.Printf("[HUB] User list queued (Count: %d)", len(users))
	default:
		log.Println("[HUB] CRITICAL: Broadcast channel full, dropping user list update")
	}
}

func (h *Hub) cleanupClient(c *Client) {
	if client, ok := h.Clients[c.Name]; ok {
		log.Printf("[HUB] Cleaning up resources for client: %s", c.Name)
		delete(h.Clients, c.Name)
		client.Conn.Close()
		close(client.Send)
		log.Printf("[HUB] Session closed for %s. Active clients remaining: %d", c.Name, len(h.Clients))
	}
}

func (h *Hub) Run() {
	log.Println("[HUB] Main loop started. Listening for events...")
	for {
		select {
		case <-h.Quit:
			log.Println("[HUB] Quit signal received. Shutting down all client connections...")
			for _, client := range h.Clients {
				h.cleanupClient(client)
			}
			return

		case client := <-h.Register:
			log.Printf("[HUB] Registration request: %s", client.Name)
			if oldClient, ok := h.Clients[client.Name]; ok {
				log.Printf("[HUB] Overwriting existing session for user: %s", client.Name)
				close(oldClient.Send)
				delete(h.Clients, client.Name)
			}

			log.Printf("[HUB] Replaying %d history messages to %s", len(h.History), client.Name)
			for _, msg := range h.History {
				payload, _ := json.Marshal(msg)
				client.Send <- payload
			}

			h.Clients[client.Name] = client
			log.Printf("[HUB] Successfully registered %s. Total active: %d", client.Name, len(h.Clients))
			h.broadcastUserList()

		case client := <-h.Unregister:
			log.Printf("[HUB] Unregistering client: %s", client.Name)
			if _, ok := h.Clients[client.Name]; ok {
				h.cleanupClient(client)
				h.broadcastUserList()
			}

		case message := <-h.Broadcast:
			payload, _ := json.Marshal(message)

			switch message.Type {
			case TypeChat, TypeSystem, TypeUserList:
				log.Printf("[HUB] Broadcasting %s message from %s", message.Type, message.Sender)
				for _, client := range h.Clients {
					select {
					case client.Send <- payload:
					default:
						log.Printf("[HUB] WARNING: Client %s buffer full. Evicting slow consumer.", client.Name)
						go func(c *Client) { h.Unregister <- c }(client)
					}
				}

				if message.Type == TypeChat {
					h.History = append(h.History, *message)
					if len(h.History) > 20 {
						h.History = h.History[1:]
					}
				}

			case TypePrivate:
				log.Printf("[HUB] Routing private message: %s -> %s", message.Sender, message.Target)
				if sender, ok := h.Clients[message.Sender]; ok {
					sender.Send <- payload
				}
				if message.Target != message.Sender {
					if target, ok := h.Clients[message.Target]; ok {
						target.Send <- payload
						log.Printf("[HUB] Private message delivered to %s", message.Target)
					} else {
						log.Printf("[HUB] Private message failed: Target %s offline", message.Target)
					}
				}
			}
		}
	}
}
