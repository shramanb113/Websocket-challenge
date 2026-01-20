package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tasks "websocket-challenge/internal/Tasks"
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
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")

		return origin == "https://localhost:5173"
	},
}

func serveWS(h *chat.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		val := r.Context().Value(middleware.UserIDKey)
		user, ok := val.(*models.User)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[WS] Upgrade error for %s: %v", user.Username, err)
			return
		}

		if user.IsBanned {
			deadline := time.Now().Add(time.Second)
			msg := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Account disabled")
			conn.WriteControl(websocket.CloseMessage, msg, deadline)

			time.Sleep(200 * time.Millisecond)
			conn.Close()
			return
		}

		client := &chat.Client{
			Hub:         h,
			Conn:        conn,
			Send:        make(chan []byte, 256),
			Name:        user.Username,
			Limiter:     middleware.NewRatelimiter(5, 500*time.Millisecond),
			LastWarning: time.Now(),
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
	repoUser := repository.NewPoolConnection(pool)
	repoRefreshToken := repository.NewRefreshTokenRepo(pool)

	tokenCleaner := tasks.NewTokenCleaner(repoRefreshToken)
	tokenCleaner.Start()

	h := chat.NewHub()
	go h.Run()

	mux := http.NewServeMux()

	authMiddleWare := middleware.Authenticate(repoUser)

	// Public Routes
	mux.HandleFunc("POST /api/auth/signup", api.SignupHandler(repoUser, repoRefreshToken))
	mux.HandleFunc("POST /api/auth/login", api.LoginHandler(repoUser, repoRefreshToken))

	// The Refresh Route (Must match the cookie path)
	mux.HandleFunc("POST /api/auth/refresh", api.RefreshHandler(repoRefreshToken, repoUser))

	// Protected Routes
	mux.HandleFunc("GET /api/auth/logout", api.Logouthandler(repoRefreshToken))
	mux.Handle("/ws", authMiddleWare(http.HandlerFunc(serveWS(h))))
	handlerWithCORS := middleware.CorsMiddleware(mux)

	certFile := "localhost+2.pem"
	keyFile := "localhost+2-key.pem"

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("🚀 Modular Server starting on :8080...")
		if err := http.ListenAndServeTLS(":8080", certFile, keyFile, handlerWithCORS); err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("ListenAndServe: %v", err)
			}
		}
	}()

	<-stop

	fmt.Println("\nShutdown signal received. Cleaning up...")
	close(h.Quit)

	fmt.Println("📦 Closing database connection pool...")
	pool.Close()

	time.Sleep(1 * time.Second)
	fmt.Println("Graceful shutdown complete. Goodnight!")
}
