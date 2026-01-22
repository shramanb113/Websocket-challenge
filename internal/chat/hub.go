package chat

import (
	"encoding/json"
	"log"
	"sync"
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
	RoomID    string      `json:"roomID"`
	Sender    string      `json:"sender"`
	Content   string      `json:"content"`
	Type      MessageType `json:"type"`
	Target    string      `json:"target,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

type Hub struct {
	mu         sync.RWMutex
	AllClients map[string]*Client
	Rooms      map[string]map[*Client]bool
	History    map[string][]Message
	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan *Message
	Quit       chan struct{}
}

type Client struct {
	RoomID      string
	Conn        *websocket.Conn
	Name        string
	Send        chan []byte
	Hub         *Hub
	Limiter     *middleware.RateLimiter
	LastWarning time.Time
	once        sync.Once
}

func NewHub() *Hub {
	log.Printf("Initializing new instance of HUB ....")
	return &Hub{
		AllClients: make(map[string]*Client),
		Rooms:      make(map[string]map[*Client]bool),
		History:    make(map[string][]Message),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Broadcast:  make(chan *Message),
		Quit:       make(chan struct{}),
	}
}

func (h *Hub) getConnectedUsers(roomId string) []string {
	users := make([]string, 0, len(h.Rooms[roomId]))
	for name := range h.Rooms[roomId] {
		users = append(users, name.Name)
	}
	return users
}

func (h *Hub) broadcastUserList(roomId string) {
	users := h.getConnectedUsers(roomId)
	rawList, _ := json.Marshal(users)

	message := &Message{
		RoomID:    roomId,
		Sender:    "SYSTEM",
		Content:   string(rawList),
		Type:      TypeUserList,
		Timestamp: time.Now().Unix(),
	}
	payload, _ := json.Marshal(message)

	if room, ok := h.Rooms[roomId]; ok {
		for client := range room {
			select {
			case client.Send <- payload:
			default:
				go func(c *Client) { h.Unregister <- c }(client)
			}
		}
	}
}

func (h *Hub) cleanupClient(c *Client) {
	c.once.Do(func() {
		if room, ok := h.Rooms[c.RoomID]; ok {
			delete(room, c)
			if len(room) == 0 {
				delete(h.Rooms, c.RoomID)
				delete(h.History, c.RoomID)
			}
		}

		if currentClient, ok := h.AllClients[c.Name]; ok && currentClient == c {
			delete(h.AllClients, c.Name)
		}

		c.Conn.Close()
		close(c.Send)
		log.Printf("[HUB] Cleanup complete for %s in %s", c.Name, c.RoomID)
	})
}

func (h *Hub) Run() {
	log.Println("[HUB] Main loop started. Listening for events...")
	for {
		select {
		case <-h.Quit:
			log.Println("[HUB] Quit signal received. Shutting down all client connections...")
			for _, client := range h.AllClients {
				h.cleanupClient(client)
			}
			return

		case client := <-h.Register:
			log.Printf("[HUB] Registration request: %s", client.Name)
			if oldClient, ok := h.AllClients[client.Name]; ok {
				log.Printf("[HUB] Overwriting existing session for user: %s", client.Name)
				h.cleanupClient(oldClient)
			}

			// FIX: Unified initialization to ensure History and Room state are synced
			if _, ok := h.Rooms[client.RoomID]; !ok {
				h.Rooms[client.RoomID] = make(map[*Client]bool)
				h.History[client.RoomID] = make([]Message, 0, 21)
			}

			log.Printf("[HUB] Replaying %d history messages to %s", len(h.History[client.RoomID]), client.Name)

			h.mu.RLock()
			historySnapshot := make([]Message, len(h.History[client.RoomID]))
			copy(historySnapshot, h.History[client.RoomID])
			h.mu.RUnlock()

			go func(c *Client, history []Message) {
				for _, msg := range history {
					payload, _ := json.Marshal(msg)
					select {
					case c.Send <- payload:
					case <-time.After(1 * time.Second):
						return
					}
				}
			}(client, historySnapshot)

			h.AllClients[client.Name] = client
			h.Rooms[client.RoomID][client] = true

			log.Printf("[HUB] Successfully registered %s. Total active in room : %d", client.Name, len(h.Rooms[client.RoomID]))

			joinMsg := &Message{
				RoomID:    client.RoomID,
				Sender:    "SYSTEM",
				Content:   client.Name + " joined the chat",
				Type:      TypeSystem,
				Timestamp: time.Now().Unix(),
			}
			joinPayload, _ := json.Marshal(joinMsg)
			for c := range h.Rooms[client.RoomID] {
				select {
				case c.Send <- joinPayload:
				default:
					continue
				}
			}

			h.broadcastUserList(client.RoomID)

		case client := <-h.Unregister:
			h.cleanupClient(client)

			if room, ok := h.Rooms[client.RoomID]; ok {
				if len(room) == 0 {
					delete(h.Rooms, client.RoomID)
					delete(h.History, client.RoomID)
					log.Printf("[HUB] Room %s is now empty and has been pruned", client.RoomID)
				} else {
					h.broadcastUserList(client.RoomID)
				}
			}

		case message := <-h.Broadcast:
			payload, _ := json.Marshal(message)

			switch message.Type {
			case TypeChat, TypeSystem, TypeUserList:
				log.Printf("[HUB] Broadcasting %s message from %s", message.Type, message.Sender)
				for client := range h.Rooms[message.RoomID] {
					select {
					case client.Send <- payload:
					default:
						log.Printf("[HUB] WARNING: Client %s buffer full. Evicting slow consumer.", client.Name)
						go func(c *Client) { h.Unregister <- c }(client)
					}
				}

				if message.Type == TypeChat {
					h.mu.Lock()
					h.History[message.RoomID] = append(h.History[message.RoomID], *message)
					if len(h.History[message.RoomID]) > 20 {
						// FIX: Zero out the first element to allow GC to clean up string memory/pointers
						h.History[message.RoomID][0] = Message{}
						h.History[message.RoomID] = h.History[message.RoomID][1:]
					}
					h.mu.Unlock()
				}

			case TypePrivate:
				log.Printf("[HUB] Routing private message: %s -> %s (Room: %s)", message.Sender, message.Target, message.RoomID)

				target, targetOk := h.AllClients[message.Target]
				sender, senderOk := h.AllClients[message.Sender]

				if targetOk && target.RoomID == message.RoomID {
					select {
					case target.Send <- payload:
						log.Printf("[HUB] Private message delivered to %s", message.Target)
					default:
						go func(c *Client) { h.Unregister <- c }(target)
					}

					if senderOk && message.Target != message.Sender {
						select {
						case sender.Send <- payload:
						default:
							go func(c *Client) { h.Unregister <- c }(sender)
						}
					}
				} else {
					log.Printf("[HUB] Private message failed: %s not in room %s", message.Target, message.RoomID)
					if senderOk {
						errorMsg := &Message{
							Sender:  "SYSTEM",
							Content: "User " + message.Target + " is not in this room.",
							Type:    TypeSystem,
							RoomID:  message.RoomID,
						}
						errPayload, _ := json.Marshal(errorMsg)
						sender.Send <- errPayload
					}
				}
			}
		}
	}
}
