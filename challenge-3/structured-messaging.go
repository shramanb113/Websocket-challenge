//structures messaging parsing proper json  also challenge 2

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
	Type    MessageType `json:"type"`
	Sender  string      `json:"sender"`
	Content string      `json:"content"`
	Target  string      `json:"target,omitempty"`
}

type Hub struct {
	clients    map[string]*Client
	broadcast  chan Message
	unregister chan *Client
	register   chan *Client
}
type Client struct {
	name string
	conn *websocket.Conn
	hub  *Hub
	send chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		broadcast:  make(chan Message, 256),
		unregister: make(chan *Client),
		register:   make(chan *Client),
	}
}

func (h *Hub) getConnectedUsers() []string {
	names := make([]string, 0, len(h.clients))
	for name := range h.clients {
		names = append(names, name)
	}
	return names
}

func (h *Hub) broadcastUserList() {
	users := h.getConnectedUsers()
	msg := Message{
		Type:    "user_list",
		Sender:  "SYSTEM",
		Content: "",
	}
	rawList, _ := json.Marshal(users)
	msg.Content = string(rawList)

	go func() { h.broadcast <- msg }()
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client.name] = client
			h.broadcastUserList()
			fmt.Printf("Total clients connected: %d\n", len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client.name]; ok {
				delete(h.clients, client.name)
				close(client.send)
				h.broadcastUserList()
				fmt.Printf("%s left. Total: %d\n", client.name, len(h.clients))
			}

		case message := <-h.broadcast:
			payload, err := json.Marshal(message)
			if err != nil {
				log.Printf("Marshal error: %v", err)
				continue
			}

			switch message.Type {
			case TypeChat:
				for _, client := range h.clients {
					select {
					case client.send <- payload:
					default:
						h.cleanupClient(client)
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

func (h *Hub) cleanupClient(c *Client) {

	if _, ok := h.clients[c.name]; ok {
		delete(h.clients, c.name)
		close(c.send)
		c.conn.Close()
	}

}

// server writing to browser

func (c *Client) writePump() {

	t := time.NewTicker(20 * time.Second)

	defer func() {
		t.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:

			c.conn.SetWriteDeadline(time.Now().Add(time.Second * 10))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-t.C:
			c.conn.SetWriteDeadline(time.Now().Add(time.Second * 10))
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second*20)); err != nil {
				log.Print("Ping Failed")
			}
			log.Print("Ping sent")
		}

	}

}

// server is reading from broweser .. so server will readmessage from browser

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(time.Second * 30))

	c.conn.SetPongHandler(func(appData string) error {
		c.conn.SetReadDeadline(time.Now().Add(time.Second * 30))
		return nil
	})

	for {
		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, message, err := c.conn.ReadMessage()

		if err != nil {
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveWS(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		conn, err := upgrader.Upgrade(w, r, nil)

		if err != nil {

			log.Fatal("Not upgrading to TCP websocket connection")
			return
		}

		client := &Client{
			name: generateRandomName(),
			send: make(chan []byte, 256),
			conn: conn,
			hub:  hub,
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

	fmt.Println("Server starting on port 8080...")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}

}
