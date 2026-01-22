package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

/*TYPES DEFINITON */

type MessageType string

const (
	TypeChat     MessageType = "chat"
	TypePrivate  MessageType = "private"
	TypeSystem   MessageType = "system"
	TypeUserList MessageType = "user_list"
)

const (
	burstLimit = 5
	refillRate = 500 * time.Millisecond
)

type Message struct {
	Sender    string      `json:"sender"`
	Content   string      `json:"content"`
	Type      MessageType `json:"type"`
	Target    string      `json:"target,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

type Hub struct {
	clients    map[string]*Client
	history    []Message
	register   chan *Client
	unregister chan *Client
	broadcast  chan *Message
	quit       chan struct{}
}

type Client struct {
	conn    *websocket.Conn
	name    string
	send    chan []byte
	hub     *Hub
	limiter *RateLimiter
}

type RateLimiter struct {
	tokens   int32
	lastTick int64
	burst    int32
	rate     time.Duration
	mu       sync.RWMutex
}

/* New Instance generation */

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		history:    make([]Message, 0),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *Message, 256),
		quit:       make(chan struct{}),
	}

}

func NewRateLimiter(burst int32, rate time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:   burst,
		lastTick: time.Now().UnixNano(),
		burst:    burst,
		rate:     rate,
	}
}

/*HUB Helper Functions */

func (h *Hub) getConnectedUsers() []string {
	clients := make([]string, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	return clients
}

func (h *Hub) broadcastUserList() {
	users := h.getConnectedUsers()
	rawList, err := json.Marshal(users)
	if err != nil {
		fmt.Printf("Error marshaling user list: %v\n", err)
		return
	}

	message := &Message{
		Sender:    "SYSTEM",
		Content:   string(rawList),
		Type:      TypeUserList,
		Timestamp: time.Now().Unix(),
	}

	select {
	case h.broadcast <- message:
	default:
		fmt.Println("Hub broadcast channel full, skipping user list update")
	}
}

func (h *Hub) cleanupClient(c *Client) {
	if client, ok := h.clients[c.name]; ok {
		delete(h.clients, c.name)
		client.conn.Close()
		close(client.send)
	}
}

/* RATE LIMITER Leaky Bucket */

func (l *RateLimiter) Allow() bool {
	new := time.Now().UnixNano()

	limit := atomic.LoadInt64(&l.lastTick)

	elapsed := new - limit

	generated := int32(elapsed / int64(l.rate))

	if generated > 0 {

		if atomic.CompareAndSwapInt64(&l.lastTick, limit, new) {
			current := atomic.LoadInt32(&l.tokens)
			newBalance := current + generated

			if newBalance >= l.burst {
				newBalance = l.burst
			}

			atomic.StoreInt32(&l.tokens, newBalance)

		}
	}

	for {
		current := atomic.LoadInt32(&l.tokens)
		if current <= 0 {
			return false
		}

		if atomic.CompareAndSwapInt32(&l.tokens, current, current-1) {
			return true
		}
	}

}

/* HUB LOGIC */

func (h *Hub) Run() {
	for {
		select {
		case <-h.quit:
			for _, client := range h.clients {
				h.cleanupClient(client)
			}

		case client := <-h.register:
			if oldClient, ok := h.clients[client.name]; ok {
				close(oldClient.send)
				delete(h.clients, client.name)
			}

			for _, message := range h.history {
				payload, err := json.Marshal(message)
				if err != nil {
					fmt.Printf("Marshal error %s", err)
					return
				}
				client.send <- payload
			}

			h.clients[client.name] = client

			fmt.Printf("Total clients: %d | User Joined: %s\n", len(h.clients), client.name)
			h.broadcastUserList()

		case client := <-h.unregister:
			if _, ok := h.clients[client.name]; ok {
				h.cleanupClient(client)
				fmt.Printf("total connected clients : %d , user left : %s\n", len(h.clients), client.name)
				h.broadcastUserList()
			}
		case message := <-h.broadcast:
			payload, err := json.Marshal(message)
			if err != nil {
				fmt.Printf("Marshal error %s", err)
				return
			}
			switch message.Type {
			case TypeChat, TypeSystem, TypeUserList:
				for _, client := range h.clients {
					select {
					case client.send <- payload:
					default:
						go func(c *Client) {
							fmt.Printf("⚠️  Dropping slow client: %s (Buffer Full)\n", c.name)
							h.unregister <- c
						}(client)
					}
				}

				if message.Type == TypeChat {
					h.history = append(h.history, *message)
					if len(h.history) > 20 {
						h.history = h.history[1:]
					}
				}

			case TypePrivate:
				if sender, ok := h.clients[message.Sender]; ok {
					sender.send <- payload
				}

				if message.Target != message.Sender {
					if target, ok := h.clients[message.Target]; ok {
						target.send <- payload
					}
				}
			default:
				log.Printf("Unknown Message Type: %s", message.Type)

			}

		}
	}
}

/* CLIENT SIDE LOGIC */

// server writing to client

func (c *Client) writePump() {

	t := time.NewTicker(time.Second * 10)
	defer func() {
		t.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-t.C:
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		}
	}
}

// server reading from browser

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("unexpected close error: %v", err)
			} else {
				log.Printf("client disconnected normally or as expected: %v", err)
			}
			break
		}
		payload := &Message{}
		// early rejection better than computationally intensive for unmarshalling
		if !c.limiter.Allow() {
			m := &Message{
				Type:    TypeSystem,
				Sender:  "SYSTEM",
				Content: "TOO MANY MESSAGES SENT .... Take a CHILL PILL",
			}
			p, _ := json.Marshal(m)
			c.send <- p
			continue
		}
		if err := json.Unmarshal(message, &payload); err != nil {
			continue
		}
		if payload.Type == TypeChat && len(payload.Content) > 0 && payload.Content[0] == '/' {
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

		payload.Sender = c.name
		payload.Timestamp = time.Now().Unix()

		c.hub.broadcast <- payload
	}
}

/* SERVER SETUP */

func generateRandomName() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	seed := time.Now().UnixNano()
	b := make([]byte, 4)
	for i := range b {
		b[i] = charset[seed%int64(len(charset))]
		seed /= int64(len(charset))
	}
	return "User_" + string(b)

}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveWS(h *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)

		if err != nil {
			return
		}

		limiter := NewRateLimiter(burstLimit, refillRate)

		client := &Client{
			conn:    conn,
			hub:     h,
			name:    generateRandomName(),
			send:    make(chan []byte, 256),
			limiter: limiter,
		}

		client.hub.register <- client
		go client.writePump()
		go client.readPump()

	}
}

func main() {

	h := NewHub()

	go h.Run()

	http.HandleFunc("/ws", serveWS(h))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("🚀 Server starting on :8080...")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Printf("Listener closed: %v", err)
		}
	}()

	<-stop

	fmt.Println("\nShutdown signal received. Cleaning up...")
	close(h.quit)
	time.Sleep(1 * time.Second)
	fmt.Println("Graceful shutdown complete.")

}
