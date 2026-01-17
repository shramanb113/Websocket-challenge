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
	"websocket-challenge/internal/models"
	"websocket-challenge/internal/repository"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveWS(h *chat.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		limiter := middleware.NewRatelimiter(5, 500*time.Millisecond)
		val := r.Context().Value(middleware.UserIDKey)
		user, ok := val.(models.User)
		if !ok {
			log.Println("Context error: UserID not found or not a UUID")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Upgrade error: %v", err)
			return
		}

		if user.IsBanned {
			message := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Account disabled")
			conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second))
			time.Sleep(time.Millisecond * 100)
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")

		w.Header().Set("Access-Control-Allow-Credentials", "true")

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
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
	mux.Handle("/ws", authMiddleWare(http.HandlerFunc(serveWS(h))))
	mux.HandleFunc("GET /logout", http.HandlerFunc(api.Logouthandler()))

	handlerWithCORS := corsMiddleware(mux)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("🚀 Modular Server starting on :8080...")
		if err := http.ListenAndServe(":8080", handlerWithCORS); err != nil {
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
