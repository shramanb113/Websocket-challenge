package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"websocket-challenge/internal/api"
	"websocket-challenge/internal/chat"
	"websocket-challenge/internal/config"
	"websocket-challenge/internal/db"
	"websocket-challenge/internal/middleware"
	"websocket-challenge/internal/repository"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveWS(h *chat.Hub, repo *repository.PostgresUserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Upgrade error: %v", err)
			return
		}

		limiter := middleware.NewRatelimiter(5, 500*time.Millisecond)
		val := r.Context().Value(middleware.UserIDKey)
		userID, ok := val.(uuid.UUID)
		if !ok {
			log.Println("Context error: UserID not found or not a UUID")
			return
		}

		user, err := repo.GetUserByID(r.Context(), userID)
		if user.Username == "" || err != nil {
			log.Printf("[WS] Could not find username for ID: %s", userID)
			conn.WriteMessage(websocket.TextMessage, []byte("Error: User profile not found"))
			conn.Close()
			return
		}

		client := &chat.Client{
			Hub:     h,
			Conn:    conn,
			Send:    make(chan []byte, 256),
			Name:    user.Username,
			Limiter: limiter,
		}

		client.Hub.Register <- client

		go client.WritePump()
		go client.ReadPump()
	}
}

func main() {

	cfg := config.Load()

	pool, err := db.Connect(cfg.DatabaseURL)

	if err != nil {
		log.Fatal("Failed to connect to database:", err)
		return
	}
	repo := repository.NewPoolConnection(pool)

	h := chat.NewHub()
	go h.Run()

	mux := http.NewServeMux()
	authMiddleWare := middleware.Authenticate(repo)

	mux.HandleFunc("POST /signup", http.HandlerFunc(api.SignupHandler(repo)))
	mux.HandleFunc("POST /login", http.HandlerFunc(api.LoginHandler(repo)))
	mux.Handle("/ws", authMiddleWare(http.HandlerFunc(serveWS(h, repo))))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("🚀 Modular Server starting on :8080...")
		if err := http.ListenAndServe(":8080", mux); err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("ListenAndServe: %v", err)
			}
		}
	}()

	<-stop

	fmt.Println("📦 Closing database connection pool...")
	pool.Close()

	fmt.Println("\nShutdown signal received. Cleaning up...")
	close(h.Quit)

	time.Sleep(1 * time.Second)
	fmt.Println("Graceful shutdown complete. Goodnight!")
}
