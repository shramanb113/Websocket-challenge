package chat

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

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
