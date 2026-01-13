package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// --- TYPES AND STRUCTS ---

type MessageType string

const (
	TypeChat     MessageType = "chat"
	TypePrivate  MessageType = "private"
	TypeSystem   MessageType = "system"
	TypeUserList MessageType = "user_list"
)

type Message struct {
	Sender  string      `json:"sender"`
	Target  string      `json:"target,omitempty"`
	Type    MessageType `json:"type"`
	Content string      `json:"content"`
}

type Hub struct {
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan Message
	history    []Message
	quit       chan struct{}
}

type Client struct {
	conn *websocket.Conn
	name string
	send chan []byte
	hub  *Hub
}

// --- HUB LOGIC ---

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Message, 256),
		history:    make([]Message, 0),
		quit:       make(chan struct{}),
	}
}

func (h *Hub) getConnectedUsers() []string {
	userList := make([]string, 0, len(h.clients))
	for name := range h.clients {
		userList = append(userList, name)
	}
	return userList
}

func (h *Hub) broadcastUserList() {
	users := h.getConnectedUsers()
	rawList, _ := json.Marshal(users)

	message := Message{
		Type:    TypeUserList,
		Sender:  "SYSTEM",
		Content: string(rawList),
	}
	// Direct send to broadcast channel to keep your logic flow
	h.broadcast <- message
}

func (h *Hub) cleanupClient(c *Client) {
	if _, ok := h.clients[c.name]; ok {
		delete(h.clients, c.name)
		close(c.send)
		c.conn.Close()
	}
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.quit:
			fmt.Println("Shutting down Hub...")
			// Final cleanup of all clients
			for _, client := range h.clients {
				h.cleanupClient(client)
			}
			return

		case client := <-h.register:
			// 1. Send History
			for _, msg := range h.history {
				payload, _ := json.Marshal(msg)
				client.send <- payload
			}
			// 2. Add to map
			h.clients[client.name] = client
			fmt.Printf("total connected clients : %d , user joined : %s\n", len(h.clients), client.name)
			// 3. Update list
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
				fmt.Printf("Marshal error: %v", err)
				continue
			}

			switch message.Type {
			case TypeChat, TypeSystem, TypeUserList:
				for _, client := range h.clients {
					select {
					case client.send <- payload:
					default:
						// If send fails, clean up the specific client
						go func(c *Client) {
							fmt.Printf("⚠️  Dropping slow client: %s (Buffer Full)\n", c.name)
							h.unregister <- c
						}(client)
					}
				}

				if message.Type == TypeChat {
					h.history = append(h.history, message)
					if len(h.history) > 20 {
						h.history = h.history[1:]
					}
				}

			case TypePrivate:
				if sender, ok := h.clients[message.Sender]; ok {
					sender.send <- payload
				}
				if target, ok := h.clients[message.Target]; ok {
					target.send <- payload
				}

			default:
				log.Printf("Unknown Message Type: %s", message.Type)
			}
		}
	}
}

// --- CLIENT LOGIC ---

func (c *Client) writePump() {
	ticker := time.NewTicker(20 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

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
			break
		}
		var payload Message
		if err := json.Unmarshal(message, &payload); err != nil {
			continue
		}
		payload.Sender = c.name
		c.hub.broadcast <- payload
	}
}

// --- SERVER SETUP ---

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

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

func serveWS(h *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Upgrade error: %v", err)
			return
		}

		client := &Client{
			conn: conn,
			hub:  h,
			send: make(chan []byte, 256),
			name: generateRandomName(),
		}

		client.hub.register <- client

		go client.writePump()
		go client.readPump()
	}
}

func main() {
	hub := NewHub()
	go hub.Run()

	http.HandleFunc("/ws", serveWS(hub))

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
	close(hub.quit)
	time.Sleep(1 * time.Second)
	fmt.Println("Graceful shutdown complete.")
}
