package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"websocket-challenge/internal/chat"
	"websocket-challenge/internal/config"
	"websocket-challenge/internal/db"
	"websocket-challenge/internal/middleware"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveWS(h *chat.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Upgrade error: %v", err)
			return
		}

		limiter := middleware.NewRatelimiter(5, 500*time.Millisecond)

		client := &chat.Client{
			Hub:     h,
			Conn:    conn,
			Send:    make(chan []byte, 256),
			Name:    generateRandomName(),
			Limiter: limiter,
		}

		client.Hub.Register <- client

		go client.WritePump()
		go client.ReadPump()
	}
}

func generateRandomName() string {
	return fmt.Sprintf("User_%d", time.Now().UnixNano()%10000)
}

func main() {

	cfg := config.Load()

	pool, err := db.Connect(cfg.DatabaseURL)

	if err != nil {
		log.Fatal("Failed to connect to database:", err)
		return
	}
	defer pool.Close()

	h := chat.NewHub()
	go h.Run()

	http.HandleFunc("/ws", serveWS(h))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("🚀 Modular Server starting on :8080...")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("ListenAndServe: %v", err)
			}
		}
	}()

	<-stop

	fmt.Println("\nShutdown signal received. Cleaning up...")
	close(h.Quit)

	time.Sleep(1 * time.Second)
	fmt.Println("Graceful shutdown complete. Goodnight!")
}
