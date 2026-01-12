// Zombie connection tester (heartbeat)

package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const PING_PERIOD = 50
const PONG_WAIT = 60
const WRITE_WAIT = 10

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	unregister chan *Client
	register   chan *Client
}

type Client struct {
	conn *websocket.Conn
	hub  *Hub
	send chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		unregister: make(chan *Client),
		register:   make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			fmt.Printf("TOtal clients connected %d\n", len(h.clients))
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				fmt.Printf("\n--- Client DISCONNECTED. Total: %d ---\n", len(h.clients))
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
					fmt.Printf("Message Sent successfully \n")
				default:
					//removing slow client (buffer full)
					delete(h.clients, client)
					close(client.send)

				}

			}
		}
	}

}

//server writing to browser

func (c *Client) writePump() {
	t := time.NewTicker(time.Duration(PING_PERIOD) * time.Second)
	defer func() {
		t.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:

			if !ok {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT * time.Second))
			c.conn.WriteMessage(websocket.TextMessage, message)
		case <-t.C:
			// set a write deadline to prevent a dead connection from blocking indefinitely
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT * time.Second))
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(WRITE_WAIT*time.Second)); err != nil {
				fmt.Println("Ping failed:", err)
				c.hub.unregister <- c
				return
			}
			fmt.Println("Ping sent")
		}
	}
}

// server getting info from browser

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)

	// inital read deadline
	c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT * time.Second))

	// setiing up the pong handler to reset the read deadline every time pong is recieved
	c.conn.SetPongHandler(func(appData string) error {
		c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT * time.Second))
		return nil
	})

	for {
		// this is important to maintain the internal state
		c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT * time.Second))
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		c.hub.broadcast <- message
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveWs(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)

		if err != nil {
			fmt.Printf("Error in upgrading %s", err)
			return
		}

		client := &Client{
			conn: conn,
			send: make(chan []byte, 256),
			hub:  hub,
		}

		client.hub.register <- client
		// specific go routines for specific connection
		go client.writePump()
		go client.readPump()
	}

}

func main() {

	hub := NewHub()

	go hub.Run()

	http.HandleFunc("/ws", serveWs(hub))

	fmt.Println("Server starting on port 8080...")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}

}
