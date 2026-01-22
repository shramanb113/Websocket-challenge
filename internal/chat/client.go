package chat

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func (c *Client) WritePump() {
	ticker := time.NewTicker(10 * time.Second)
	defer func() {
		ticker.Stop()
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.Send)
			for i := 0; i < n; i++ {
				msg, ok := <-c.Send
				if !ok {
					break
				}
				w.Write([]byte{'\n'})
				w.Write(msg)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) ReadPump() {
	defer func() {
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
				log.Printf("[CLIENT] Unexpected close: %v", err)
			}
			break
		}

		// Rate Limiter
		if !c.Limiter.Allow() {
			if time.Since(c.LastWarning) > 3*time.Second {
				warning, _ := json.Marshal(&Message{
					Type: TypeSystem, Sender: "SYSTEM", Content: "⚠️ Rate limit exceeded.", RoomID: c.RoomID,
				})
				select {
				case c.Send <- warning:
					c.LastWarning = time.Now()
				default:
				}
			}
			continue
		}

		payload := &Message{}
		if err := json.Unmarshal(message, payload); err != nil {
			continue
		}

		payload.Sender = c.Name
		payload.RoomID = c.RoomID
		payload.Timestamp = time.Now().Unix()

		if payload.Type == TypeChat && len(payload.Content) > 0 && payload.Content[0] == '/' {
			parts := strings.SplitN(payload.Content, " ", 2)
			cmd := parts[0]
			rest := ""
			if len(parts) > 1 {
				rest = parts[1]
			}

			switch cmd {
			case "/help", "/ping":
				content := "Pong! 🏓"
				if cmd == "/help" {
					content = "Commands: /shrug, /lenny, /tableflip, /unflip, /bear, /disapprove, /hug, /dance, /sparkles, /flex, /cry, /coffee, /fix, /deploy, /ping"
				}

				resp, _ := json.Marshal(&Message{
					Type: TypeSystem, Sender: "SYSTEM", Content: content, RoomID: c.RoomID,
				})
				c.Send <- resp
				continue

			case "/shrug":
				payload.Content = rest + " ¯\\_(ツ)_/¯"
			case "/lenny":
				payload.Content = rest + " ( ͡° ͜ʖ ͡°)"
			case "/tableflip":
				payload.Content = rest + " (╯°□°）╯︵ ┻━┻"
			case "/unflip":
				payload.Content = rest + " ┬─┬ノ( º _ ºノ)"
			case "/bear":
				payload.Content = rest + " ʕ •ᴥ•ʔ"
			case "/disapprove":
				payload.Content = rest + " ಠ_ಠ"
			case "/hug":
				payload.Content = rest + " (づ｡◕‿‿◕｡)づ"
			case "/dance":
				payload.Content = rest + " └|∵|┐  ♪  ┌|∵|┘"
			case "/sparkles":
				payload.Content = "✨ " + strings.TrimSpace(rest) + " ✨"
			case "/flex":
				payload.Content = rest + " ᕦ(ò_ó)ᕤ"
			case "/cry":
				payload.Content = rest + " (╥﹏╥)"
			case "/coffee":
				payload.Content = rest + " ☕"
			case "/fix":
				payload.Content = rest + " 🛠️"
			case "/deploy":
				payload.Content = rest + " 🚀"
			}
		}

		c.Hub.Broadcast <- payload
	}
}
