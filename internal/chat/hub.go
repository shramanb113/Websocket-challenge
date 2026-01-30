package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"websocket-challenge/internal/hashing"
	"websocket-challenge/internal/middleware"
	"websocket-challenge/internal/models"
	"websocket-challenge/internal/repository"
	"websocket-challenge/internal/types"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type Hub struct {
	mu               sync.RWMutex
	AllClients       map[string]*Client
	Rooms            map[string]map[*Client]bool
	Register         chan *Client
	Unregister       chan *Client
	Broadcast        chan *types.Message
	Repo             repository.MessageRepo
	PersistenceQueue chan *models.Message
	Quit             chan struct{}

	ServerID    string
	RedisClient *redis.Client
	RedisPubSub *redis.PubSub
	ActiveSubs  map[string]bool

	Ring *hashing.Ring

	RedisOnline atomic.Bool

	LastSeen map[string]time.Time
}

type Client struct {
	RoomID      string
	Conn        *websocket.Conn
	Name        string
	Send        chan []byte
	Hub         *Hub
	Limiter     *middleware.RateLimiter
	LastWarning time.Time
	once        sync.Once
}

func NewConsistentHashing(replicas int) *hashing.Ring {
	return &hashing.Ring{
		Nodes:    make([]uint32, 0),
		Registry: make(map[uint32]string),
		Replicas: replicas,
	}
}

func MyHandler(h *Hub) {
	currentID := h.ServerID
	fmt.Println("This server's ID is:", currentID)
}

func NewHub(repo repository.MessageRepo, wg *sync.WaitGroup, rdb *redis.Client, publicAddr string) *Hub {

	h := &Hub{
		Repo:             repo,
		AllClients:       make(map[string]*Client),
		Rooms:            make(map[string]map[*Client]bool),
		PersistenceQueue: make(chan *models.Message, 1024),
		Broadcast:        make(chan *types.Message, 2048),
		Register:         make(chan *Client, 64),
		Unregister:       make(chan *Client, 64),
		Quit:             make(chan struct{}),

		ServerID:    publicAddr,
		RedisClient: rdb,
		RedisPubSub: rdb.Subscribe(context.Background()),
		ActiveSubs:  make(map[string]bool),

		Ring: NewConsistentHashing(50),

		RedisOnline: atomic.Bool{},

		LastSeen: make(map[string]time.Time),
	}
	log.Printf("[HUB] Main loop started on Server [%s]", h.ServerID)

	h.Ring.Add(h.ServerID)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	err := rdb.Ping(ctx).Err()
	cancel()

	if err == nil {
		h.RedisOnline.Store(true)
	}

	log.Printf("[HUB] Main loop started on Server [%s] | Redis Online: %v", h.ServerID, h.RedisOnline.Load())

	wg.Add(3)
	go h.PersistMessageWorker(wg)
	go h.ListenToRedis(wg)
	go func() {
		defer wg.Done()
		h.IsOnline()
	}()

	h.announceServerAdd()

	return h
}

func (h *Hub) IsOnline() {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
			err := h.RedisClient.Ping(ctx).Err()
			cancel()
			if err != nil {
				if h.RedisOnline.Load() {
					log.Println("🚨 REDIS OFFLINE: Hub switching to Standalone Fallback")
					h.RedisOnline.Store(false)
				}
			} else {
				if h.RedisOnline.Load() {
					h.announceServerAdd()
					h.mu.Lock()
					now := time.Now()
					for serverID, lastTime := range h.LastSeen {
						if now.Sub(lastTime) > 20*time.Second {
							log.Printf("[WATCHER] Server %s timed out. Removing from Ring.", serverID)
							h.Ring.Remove(serverID)
							delete(h.LastSeen, serverID)
						}
					}
					h.mu.Unlock()
				}
				if !h.RedisOnline.Load() {
					log.Println("✅ REDIS ONLINE: Hub resuming Cluster Mode")
					h.RedisOnline.Store(true)
					h.announceServerAdd()
					err := h.RedisPubSub.Subscribe(context.Background(), "global_signals")
					if err != nil {
						log.Printf("[REDIS ERROR] Could not subscribe to global_signals: %v", err)
					}

					for roomID := range h.ActiveSubs {
						err := h.RedisPubSub.Subscribe(context.Background(), roomID)
						if err != nil {
							log.Printf("[REDIS ERROR] Could not subscribe to roomID %s : %v", roomID, err)
						}
					}
				}

			}
		case <-h.Quit:
			return
		}
	}
}

func (h *Hub) announceServerAdd() {
	m := &types.Message{
		Sender:  "SYSTEM",
		Content: "JOIN:" + h.ServerID,
		Type:    types.TypeSystem,
	}

	messageBytes, _ := json.Marshal(m)

	h.RedisClient.Publish(context.Background(), "global_signals", messageBytes)
}

func (h *Hub) AnnounceServerLeave() {
	m := &types.Message{
		Sender:  "SYSTEM",
		Content: "LEAVE:" + h.ServerID,
		Type:    types.TypeSystem,
	}
	messageBytes, _ := json.Marshal(m)

	h.RedisClient.Publish(context.Background(), "global_signals", messageBytes)
}

func (h *Hub) ListenToRedis(wg *sync.WaitGroup) {
	defer wg.Done()

	err := h.RedisPubSub.Subscribe(context.Background(), "global_signals")
	if err != nil {
		log.Printf("[REDIS ERROR] Could not subscribe to global_signals: %v", err)
	}

	ch := h.RedisPubSub.Channel()

	for message := range ch {

		var m types.Message

		if err := json.Unmarshal([]byte(message.Payload), &m); err != nil {
			continue
		}

		if newID, found := strings.CutPrefix(m.Content, "JOIN:"); found {
			if newID != h.ServerID {
				added := h.Ring.Add(newID)
				if added {
					log.Printf("[RING] Added NEW remote server: %s", newID)
					h.mu.Lock()
					h.LastSeen[newID] = time.Now()
					h.mu.Unlock()
					h.announceServerAdd()
				}
			}
			continue
		}
		if newID, found := strings.CutPrefix(m.Content, "LEAVE:"); found {
			if newID != h.ServerID {
				removed := h.Ring.Remove(newID)
				if removed {
					log.Printf("[RING] Removed existing remote server: %s", newID)
				}
			}
			continue
		}

		if m.Type == types.TypeAck {

			h.mu.Lock()
			if client, ok := h.AllClients[m.Sender]; ok {
				log.Printf("[SIGNAL] Kicking local user %s due to global login", m.Sender)

				go h.cleanupClient(client)
			}
			h.mu.Unlock()
			continue
		}

		m.FromRedis = true

		h.Broadcast <- &m

	}
}

func (h *Hub) PersistMessageWorker(wg *sync.WaitGroup) {
	defer wg.Done()
	for msg := range h.PersistenceQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		switch msg.Type {
		case models.TypeChat, models.TypePrivate:
			if err := h.Repo.Save(ctx, msg); err != nil {
				log.Printf("Worker [SAVE] error: %v", err)
			}

		case models.TypeAck:
			if err := h.Repo.UpdateStatus(ctx, msg.ID, models.MessageStatus(msg.Status)); err != nil {
				log.Printf("Worker [UPDATE] error: %v for message id : %s", err, msg.ID)
			}

		default:
			log.Printf("Worker: Received unhandled message type: %s", msg.Type)
		}

		cancel()
	}
	log.Println("Worker: All messages processed. Shutting down.")
}

func (h *Hub) replayHistory(c *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	history, err := h.Repo.Fetch(ctx, c.RoomID, c.Name, 50, time.Now())
	if err != nil {
		log.Printf("Error fetching history: %v", err)
		return
	}

	for i := len(history) - 1; i >= 0; i-- {
		data, _ := json.Marshal(history[i])
		c.Send <- data
	}
}

func MessageToModel(m *types.Message) *models.Message {
	return &models.Message{
		ID:        m.ID,
		RoomID:    m.RoomID,
		Sender:    m.Sender,
		Target:    m.Target,
		Content:   m.Content,
		Type:      models.MessageType(m.Type),
		Timestamp: m.Timestamp,
		Status:    models.MessageStatus(m.Status),
	}
}

func (h *Hub) broadcastUserList(roomId string) {

	ctx := context.Background()
	users, err := h.RedisClient.SMembers(ctx, "room:"+roomId+":users").Result()
	if err != nil {
		log.Printf("Redis error fetching user list: %v", err)
		return
	}

	rawList, _ := json.Marshal(users)
	message := &types.Message{
		RoomID:         roomId,
		Sender:         "SYSTEM",
		Content:        string(rawList),
		Type:           types.TypeUserList,
		Timestamp:      time.Now(),
		SenderServerID: h.ServerID,
	}

	h.Broadcast <- message
}

func (h *Hub) cleanupClient(c *Client) {
	c.once.Do(func() {
		ctx := context.Background()

		h.RedisClient.SRem(ctx, "room:"+c.RoomID+":users", c.Name)

		var shouldUnsubscribe bool

		h.mu.Lock()
		if room, ok := h.Rooms[c.RoomID]; ok {
			delete(room, c)
			if len(room) == 0 {
				delete(h.Rooms, c.RoomID)
				delete(h.ActiveSubs, c.RoomID)
				shouldUnsubscribe = true
				log.Printf("[HUB] Room %s is now empty locally.", c.RoomID)
			}
		}

		if currentClient, ok := h.AllClients[c.Name]; ok && currentClient == c {
			delete(h.AllClients, c.Name)
		}
		h.mu.Unlock()

		if shouldUnsubscribe {
			if err := h.RedisPubSub.Unsubscribe(ctx, c.RoomID); err != nil {
				log.Printf("[REDIS ERROR] Failed to unsubscribe from %s: %v", c.RoomID, err)
			}
		}

		go h.broadcastUserList(c.RoomID)

		c.Conn.Close()
		close(c.Send)

		log.Printf("[HUB] Cleanup complete for %s", c.Name)
	})
}

func (h *Hub) manageSubscription(roomID string) {
	ctx := context.Background()

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.Rooms[roomID]) == 0 {
		if h.ActiveSubs[roomID] {
			h.RedisPubSub.Unsubscribe(ctx, roomID)
			delete(h.ActiveSubs, roomID)
			log.Printf("[REDIS] Room %s is empty locally. Unsubscribed.", roomID)
		}
	}
}

func (h *Hub) Run(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("[HUB] Main loop started. Listening for events...")

	for {
		select {
		case <-h.Quit:

			log.Println("[HUB] Quit signal received. Shutting down all client connections...")

			for _, client := range h.AllClients {
				h.cleanupClient(client)
			}

			if err := h.RedisPubSub.Close(); err != nil {
				log.Printf("[REDIS] Error closing PubSub: %v", err)
			}
			return

		case client := <-h.Register:
			log.Printf("[HUB] Registration request: %s", client.Name)

			h.mu.RLock()
			oldClient, exists := h.AllClients[client.Name]
			h.mu.RUnlock()

			if exists {
				log.Printf("[HUB] Overwriting existing session for user: %s", client.Name)
				h.cleanupClient(oldClient)
			}

			ctx := context.Background()

			kickMsg := &types.Message{
				Type:           types.TypeKick,
				Sender:         client.Name,
				SenderServerID: h.ServerID,
			}

			payload, _ := json.Marshal(kickMsg)

			h.RedisClient.Publish(ctx, "global_signals", payload)

			err := h.RedisClient.SAdd(ctx, "room:"+client.RoomID+":users", client.Name).Err()
			if err != nil {
				log.Printf("Redis SAdd error: %v", err)
			}

			h.mu.Lock()
			if !h.ActiveSubs[client.RoomID] {
				h.RedisPubSub.Subscribe(ctx, client.RoomID)
				h.ActiveSubs[client.RoomID] = true
			}

			if _, ok := h.Rooms[client.RoomID]; !ok {
				h.Rooms[client.RoomID] = make(map[*Client]bool)
			}
			h.AllClients[client.Name] = client
			h.Rooms[client.RoomID][client] = true
			h.mu.Unlock()

			go h.replayHistory(client)

			joinMsg := &types.Message{
				RoomID:    client.RoomID,
				Sender:    "SYSTEM",
				Content:   client.Name + " joined the chat",
				Type:      types.TypeSystem,
				Timestamp: time.Now(),
			}

			joinPayload, _ := json.Marshal(joinMsg)

			h.RedisClient.Publish(ctx, joinMsg.RoomID, joinPayload)

			count, _ := h.RedisClient.SCard(ctx, "room:"+client.RoomID+":users").Result()
			log.Printf("[HUB] Registered %s. Global count: %d", client.Name, count)

			h.broadcastUserList(client.RoomID)

		case client := <-h.Unregister:
			log.Printf("[HUB] Unregistering client: %s", client.Name)
			h.cleanupClient(client)
			h.manageSubscription(client.RoomID)
			h.broadcastUserList(client.RoomID)

		case message := <-h.Broadcast:

			if !message.FromRedis {
				log.Printf("[INGEST] Local message from %s (Room: %s, Type: %s). Sending to cluster...",
					message.Sender, message.RoomID, message.Type)

				message.SenderServerID = h.ServerID

				payload, err := json.Marshal(message)
				if err != nil {
					log.Printf("[ERROR] Failed to marshal local message: %v", err)
					continue
				}

				err = h.RedisClient.Publish(context.Background(), message.RoomID, payload).Err()
				if err != nil {
					log.Printf("[REDIS ERROR] Failed to publish message: %v", err)
					continue
				}

				if message.Type == types.TypeChat || message.Type == types.TypePrivate || message.Type == types.TypeAck {
					log.Printf("[DB-QUEUE] Queueing %s for persistence", message.Type)
					h.PersistenceQueue <- MessageToModel(message)
				}

				continue
			}

			if message.FromRedis {
				payload, err := json.Marshal(message)
				if err != nil {
					log.Printf("[ERROR] Failed to marshal message from redis: %v", err)
					continue
				}

				switch message.Type {
				case types.TypeChat, types.TypeUserList, types.TypeSystem, types.TypeTyping:
					h.mu.RLock()
					clients, roomExists := h.Rooms[message.RoomID]

					if !roomExists {
						h.mu.RUnlock()
						log.Printf("[DELIVERY] No local clients in Room %s. Skipping delivery.", message.RoomID)
						continue
					}

					for client := range clients {
						select {
						case client.Send <- payload:
						default:
							log.Printf("[HUB] WARNING: Buffer full for %s. Evicting slow consumer.", client.Name)
							go func(c *Client) { h.Unregister <- c }(client)
						}
					}

					h.mu.RUnlock()

				case types.TypePrivate:
					h.mu.RLock()
					target, targetOk := h.AllClients[message.Target]
					sender, senderOk := h.AllClients[message.Sender]
					h.mu.RUnlock()

					if targetOk && target.RoomID == message.RoomID {
						select {
						case target.Send <- payload:
							log.Printf("[PRIVATE] Delivered to target: %s", message.Target)

						default:
							go func(c *Client) { h.Unregister <- c }(target)
						}
					}

					// multiple devices( have to reject the frontend from displaying this message in the screen and just listen)
					if senderOk && sender.RoomID == message.RoomID {
						select {
						case sender.Send <- payload:
							log.Printf("[PRIVATE] Delivered back to sender for sync: %s", message.Sender)
						default:
							go func(c *Client) { h.Unregister <- c }(sender)
						}
					}

				case types.TypeAck:
					h.mu.RLock()
					originalSender, ok := h.AllClients[message.Target]
					h.mu.RUnlock()

					if ok {
						select {
						case originalSender.Send <- payload:
							log.Printf("[ACK] Status update sent to %s for Msg %s", message.Target, message.ID)
						default:
							go func(c *Client) { h.Unregister <- c }(originalSender)
						}
					}

				}
			}
		}
	}
}
