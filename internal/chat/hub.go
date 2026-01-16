package chat

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"websocket-challenge/internal/middleware"
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

func (c *Client) WritePump() {
	log.Printf("[CLIENT] Starting WritePump for %s", c.Name)
	t := time.NewTicker(10 * time.Second)
	defer func() {
		log.Printf("[CLIENT] Stopping WritePump for %s", c.Name)
		t.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if !ok {
				log.Printf("[CLIENT] Hub closed the send channel for %s", c.Name)
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[CLIENT] Write error for %s: %v", c.Name, err)
				return
			}
		case <-t.C:
			c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[CLIENT] Heartbeat ping failed for %s", c.Name)
				return
			}
		}
	}
}

func (c *Client) ReadPump() {
	log.Printf("[CLIENT] Starting ReadPump for %s", c.Name)
	defer func() {
		log.Printf("[CLIENT] Stopping ReadPump for %s", c.Name)
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[CLIENT] Unexpected close for %s: %v", c.Name, err)
			}
			break
		}

		if !c.Limiter.Allow() {
			log.Printf("[SECURITY] Rate limit triggered for %s. Dropping message.", c.Name)
			m := &Message{Type: TypeSystem, Sender: "SYSTEM", Content: "⚠️ Rate limit exceeded. Slow down!"}
			p, _ := json.Marshal(m)
			c.Send <- p
			continue
		}

		payload := &Message{}
		if err := json.Unmarshal(message, payload); err != nil {
			log.Printf("[CLIENT] Unmarshal error for %s: %v", c.Name, err)
			continue
		}

		if payload.Type == TypeChat && len(payload.Content) > 0 && payload.Content[0] == '/' {
			log.Printf("[CLIENT] Parsing command from %s: %s", c.Name, payload.Content)
			parts := strings.SplitN(payload.Content, " ", 2)
			command := parts[0]
			rest := ""
			if len(parts) > 1 {
				rest = parts[1] + " "
			}

			switch command {
			case "/shrug":
				payload.Content = rest + "¯\\_(ツ)_/¯"
			case "/lenny":
				payload.Content = rest + "( ͡° ͜ʖ ͡°)"
			case "/tableflip":
				payload.Content = rest + "(╯°□°）╯︵ ┻━┻"
			case "/unflip":
				payload.Content = rest + "┬─┬ノ( º _ ºノ)"
			case "/bear":
				payload.Content = rest + "ʕ •ᴥ•ʔ"
			case "/disapprove":
				payload.Content = rest + "ಠ_ಠ"
			case "/hug":
				payload.Content = rest + "(づ｡◕‿‿◕｡)づ"
			case "/dance":
				payload.Content = rest + "└|∵|┐  ♪  ┌|∵|┘"
			case "/sparkles":
				payload.Content = "✨ " + strings.TrimSpace(rest) + " ✨"
			case "/flex":
				payload.Content = rest + "ᕦ(ò_ó)ᕤ"
			case "/cry":
				payload.Content = rest + "(╥﹏╥)"
			case "/coffee":
				payload.Content = rest + "☕ Fueling the developer..."
			case "/fix":
				payload.Content = rest + "🛠️ It's not a bug, it's a feature!"
			case "/deploy":
				payload.Content = rest + "🚀 Ship it!"
			case "/ping":
				payload.Content = "Pong! 🏓"
			case "/help":
				payload.Type = TypeSystem
				payload.Content = "Commands: /shrug, /lenny, /tableflip, /unflip, /bear, /disapprove, /hug, /dance, /sparkles, /flex, /cry, /coffee, /fix, /deploy"
			}
		}

		payload.Sender = c.Name
		payload.Timestamp = time.Now().Unix()
		c.Hub.Broadcast <- payload
	}
}
