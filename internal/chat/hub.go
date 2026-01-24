package chat

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"websocket-challenge/internal/middleware"
	"websocket-challenge/internal/models"
	"websocket-challenge/internal/repository"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type MessageType string

const (
	TypeChat     MessageType = "chat"
	TypePrivate  MessageType = "private"
	TypeSystem   MessageType = "system"
	TypeUserList MessageType = "user_list"
	TypeAck      MessageType = "user_ack"
	TypeTyping   MessageType = "user_typing"
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
}

type Hub struct {
	mu               sync.RWMutex
	AllClients       map[string]*Client
	Rooms            map[string]map[*Client]bool
	Register         chan *Client
	Unregister       chan *Client
	Broadcast        chan *Message
	Repo             repository.MessageRepo
	PersistenceQueue chan *models.Message
	Quit             chan struct{}
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

func NewHub(repo repository.MessageRepo, wg *sync.WaitGroup) *Hub {
	log.Printf("Initializing new instance of HUB ....")
	h := &Hub{
		Repo:             repo,
		AllClients:       make(map[string]*Client),
		Rooms:            make(map[string]map[*Client]bool),
		PersistenceQueue: make(chan *models.Message, 1024),
		Register:         make(chan *Client),
		Unregister:       make(chan *Client),
		Broadcast:        make(chan *Message),
		Quit:             make(chan struct{}),
	}

	wg.Add(1)
	go h.PersistMessageWorker(wg)

	return h
}
func (h *Hub) PersistMessageWorker(wg *sync.WaitGroup) {
	defer wg.Done()
	for msg := range h.PersistenceQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		switch msg.Type {
		case models.TypeChat, models.TypePrivate:
			if err := h.Repo.Save(ctx, msg); err != nil {
				log.Printf("Worker [SAVE] error: %v", err)
			}

		case models.TypeAck:
			if err := h.Repo.UpdateStatus(ctx, msg.ID, models.MessageStatus(msg.Status)); err != nil {
				log.Printf("Worker [UPDATE] error: %v for message id : %s", err, msg.ID)
			}

		default:
			log.Printf("Worker: Received unhandled message type: %s", msg.Type)
		}

		cancel()
	}
	log.Println("Worker: All messages processed. Shutting down.")
}

func (h *Hub) replayHistory(c *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	history, err := h.Repo.Fetch(ctx, c.RoomID, c.Name, 50, time.Now())
	if err != nil {
		log.Printf("Error fetching history: %v", err)
		return
	}

	for i := len(history) - 1; i >= 0; i-- {
		data, _ := json.Marshal(history[i])
		c.Send <- data
	}
}

func (m *Message) ToModel() *models.Message {
	return &models.Message{
		ID:        m.ID,
		RoomID:    m.RoomID,
		Sender:    m.Sender,
		Target:    m.Target,
		Content:   m.Content,
		Type:      models.MessageType(m.Type),
		Timestamp: m.Timestamp,
		Status:    models.MessageStatus(m.Status),
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
		Timestamp: time.Now(),
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
		h.mu.Lock()
		defer h.mu.Unlock()

		if room, ok := h.Rooms[c.RoomID]; ok {
			delete(room, c)
			if len(room) == 0 {
				delete(h.Rooms, c.RoomID)
				log.Printf("[HUB] Room %s is now empty and removed ", c.RoomID)
			}
		}

		if currentClient, ok := h.AllClients[c.Name]; ok && currentClient == c {
			delete(h.AllClients, c.Name)
		}

		c.Conn.Close()
		close(c.Send)

		log.Printf("[HUB] Cleanup complete for %s", c.Name)
	})
}

func (h *Hub) Run(wg *sync.WaitGroup) {
	defer wg.Done()
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

			if _, ok := h.Rooms[client.RoomID]; !ok {
				h.Rooms[client.RoomID] = make(map[*Client]bool)
			}

			h.AllClients[client.Name] = client
			h.Rooms[client.RoomID][client] = true

			go h.replayHistory(client)

			log.Printf("[HUB] Successfully registered %s. Total active in room : %d", client.Name, len(h.Rooms[client.RoomID]))

			joinMsg := &Message{
				RoomID:    client.RoomID,
				Sender:    "SYSTEM",
				Content:   client.Name + " joined the chat",
				Type:      TypeSystem,
				Timestamp: time.Now(),
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
			if clients, ok := h.Rooms[client.RoomID]; ok {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client.Send)

					if len(clients) == 0 {
						delete(h.Rooms, client.RoomID)
						log.Printf("Room %s is now empty and removed from memory", client.RoomID)
					}
				}
			}

		case message := <-h.Broadcast:
			payload, _ := json.Marshal(message)

			switch message.Type {
			case TypeChat, TypeSystem, TypeUserList, TypeTyping:
				log.Printf("[HUB] Broadcasting %s message from %s", message.Type, message.Sender)
				for client := range h.Rooms[message.RoomID] {

					if message.Sender == client.Name {
						continue
					}

					select {
					case client.Send <- payload:
					default:
						log.Printf("[HUB] WARNING: Client %s buffer full. Evicting slow consumer.", client.Name)
						go func(c *Client) { h.Unregister <- c }(client)
					}
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
			case TypeAck:
				log.Printf("[HUB] Ack received: Msg %s is now status %d", message.ID, message.Status)
			}
			if message.Type == TypeChat || message.Type == TypePrivate || message.Type == TypeAck {
				h.PersistenceQueue <- message.ToModel()
			}
		}
	}
}
