package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type MessageType string

const (
	TypeChat    MessageType = "chat"
	TypePrivate MessageType = "private"
	TypeSystem  MessageType = "system"
)

type Message struct {
	Sender  string      `json:"sender"`
	Target  string      `json:"target,omitempty"`
	Type    MessageType `json:"type"`
	Content string      `json:"content"`
}

type Hub struct {
	clients    map[string]*Client
	unregister chan *Client
	register   chan *Client
	broadcast  chan Message
	history    []Message
}

type Client struct {
	conn *websocket.Conn
	name string
	hub  *Hub
	send chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Message, 256),
		history:    make([]Message, 0),
	}
}

func (h *Hub) getConnectedUsers() []string {
	clients := make([]string, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)

	}
	return clients
}

func (h *Hub) broadcastUserList() {
	users := h.getConnectedUsers()
	message := &Message{
		Type:    "user_list",
		Content: "",
		Sender:  "SYSTEM",
	}
	rawList, _ := json.Marshal(users)
	message.Content = string(rawList)
	go func() { h.broadcast <- *message }()
}

// IMPORTANT
// only closing the sender channel in HUB
func (h *Hub) cleanupClient(c *Client) {
	delete(h.clients, c.name)
	close(c.send)
	c.conn.Close()
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			for _, message := range h.history {
				mess, _ := json.Marshal(message)

				client.send <- mess
			}
			h.clients[client.name] = client
			h.broadcastUserList()
			fmt.Printf("total connected clients : %d\n", len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client.name]; ok {
				h.cleanupClient(client)
				h.broadcastUserList()
			}
			fmt.Printf("total connected clients : %d , left = %s", len(h.clients), client.name)

		case message := <-h.broadcast:
			payload, err := json.Marshal(message)
			if err != nil {
				log.Printf("Marshal error: %v", err)
				continue
			}

			switch message.Type {
			case TypeChat, "user_list", TypeSystem:
				for _, client := range h.clients {
					select {
					case client.send <- payload:
					default:
						h.cleanupClient(client)
					}
				}
				if message.Type == TypeChat {
					h.history = append(h.history, message)
					if len(h.history) > 20 {
						h.history = h.history[1:]
					}
				}

			case TypePrivate:
				if target, ok := h.clients[message.Target]; ok {
					target.send <- payload
				}
				if sender, ok := h.clients[message.Sender]; ok {
					sender.send <- payload
				}
			default:
				log.Printf("Unknown Message Type: %s", message.Type)

			}
		}

	}
}

// server writes to browser

func (c *Client) writePump() {

	ticker := time.NewTicker(20 * time.Second)

	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	c.conn.SetWriteDeadline(time.Now().Add(time.Second * time.Duration(20)))

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(time.Second * time.Duration(20)))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second*time.Duration(20))); err != nil {
				fmt.Print("Ping Failed")
				return
			}
			fmt.Print("Ping successful")
		}
	}

}

// server reading from message
func (c *Client) readPump() {

	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(time.Second * time.Duration(30)))

	c.conn.SetPongHandler(func(appData string) error {
		c.conn.SetReadDeadline(time.Now().Add(time.Second * time.Duration(30)))
		return nil
	})
	for {

		_, message, err := c.conn.ReadMessage()

		if err != nil {
			fmt.Print("Error reading from client")
			return
		}
		var payload Message

		json.Unmarshal(message, &payload)

		payload.Sender = c.name

		c.hub.broadcast <- payload

	}

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
			log.Fatal("Http connection into upgraded")
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func main() {

	hub := NewHub()

	go hub.Run()

	http.HandleFunc("/ws", serveWS(hub))

	fmt.Println("Server starting on port 8080...")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}

}
