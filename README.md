# 🚀 GoHub: 20-Step Distributed Messaging Engine

> **Status: Challenge 6/20 Complete** > A high-concurrence, thread-safe real-time communication server built with **Go** and **Gorilla WebSockets**. Demonstrating advanced systems engineering: non-blocking backpressure, graceful termination, and distributed command parsing.

---

## 🛠️ The Journey So Far

### Challenge 1: The Hub-and-Spoke Concurrency Model

**Problem Statement:** Manage state across hundreds of simultaneous connections without data races.

- **The Struggle:** Traditional mutex locking on a global client map becomes a bottleneck as concurrency scales.
- **The Win:** Utilized the **CSP (Communicating Sequential Processes)** pattern. Designed a central `Hub` that manages state via Go channels, ensuring the `clients` map is only modified by a single goroutine.

### Challenge 2: Bidirectional Data Flow (The Pumps)

**Problem Statement:** Decouple reading from writing to prevent network I/O from blocking application logic.

- **The Struggle:** Independent I/O streams can hang if one side waits for the other.
- **The Win:** Designed `readPump` and `writePump` goroutines for every client. Independent processing of incoming messages and outgoing heartbeats (pings/pongs) maximizes throughput.

### Challenge 3: Non-Blocking Backpressure (The Deadlock Guard)

**Problem Statement:** Prevent a "Slow Consumer" from filling its buffer and causing the entire broadcast loop to hang.

- **The Struggle:** A single lagging client could potentially stall the entire system.
- **The Win:** Implemented a `select` statement with a `default` case during broadcasting. If a client's buffer is full, the server drops the client asynchronously, protecting system latency.

### Challenge 4: Private Messaging & Command Parsing

**Problem Statement:** Implementing targeted delivery and scalable message modification (e.g., `/shrug`).

- **The Struggle:** Centralized parsing in the Hub creates a CPU bottleneck.
- **The Win:** Offloaded parsing logic to the `readPump` level. By using `strings.SplitN` at the client level, CPU load is distributed across all available cores before reaching the Hub.

### Challenge 5: Graceful Process Lifecycle (SIGTERM Handling)

**Problem Statement:** Prevent orphaned connections and data loss during server restarts or shutdowns.

- **The Struggle:** Hard-killing the process drops client state instantly without cleanup.
- **The Win:** Integrated OS signal monitoring (`SIGTERM`, `os.Interrupt`). The server initiates a "Drain" sequence, notifying clients and cleaning up resources gracefully before exiting.

### Challenge 6: The "Bulletproof" Sprint (Reliability & A11y)

**Problem Statement:** Making the system resilient to real-world network instability (Offline/Online toggles) and WCAG compliance.

- **The Struggle:** \* **Zombie Connections:** Reconnections created "Ghost" sessions in the Hub, leading to double-delivery and delivery to dead sockets.
  - **Silent Failures:** Browsers failing to recognize "Half-Open" TCP sockets during network switching.
- **The Win:**
  - **Anti-Zombie Registration:** Implemented a check that kicks existing connections with the same ID before allowing a new session.
  - **Watchdog Heartbeat:** Added a client-side watchdog timer and `window` event listeners for instant "Offline" UI state transitions.
  - **Compatibility:** Implemented `-webkit-backdrop-filter` polyfills and ARIA labeling for a compliant, premium UI.

# Phase 2: Security & Traffic Control

## Challenge 7: Atomic Traffic Control (The Gatekeeper)

**Problem Statement:** Protect the server from CPU exhaustion and "Message Flooding" (DDoS) without introducing lock contention.

**The Struggle:** Traditional rate limiting often uses a `sync.Mutex`, which can become a major bottleneck in high-concurrency environments as goroutines fight for the lock.

**The Win:** Implemented a high-performance **Atomic Leaky Bucket** rate limiter.

- **Lock-Free Scaling:** Utilized `sync/atomic` with a `CompareAndSwap` loop to manage tokens and timestamps, ensuring thread-safety with near-zero overhead.
- **Early Rejection Pattern:** Positioned the `limiter.Allow()` check before the expensive `json.Unmarshal` operation. This ensures that malicious or spammy traffic is rejected at the byte level, saving precious CPU cycles.
- **Lazy Refilling:** Designed the limiter to calculate token refills "on-demand" during the check, rather than running a background timer for every single user, significantly reducing memory footprint.

---

## Challenge 8: The Stateless Sentinel (JWT & HttpOnly Cookies)

**Problem Statement:** Securely manage user sessions without storing state on the server (like sessions in Redis or DB) while protecting against XSS and CSRF attacks.

**The Struggle:** LocalStorage is vulnerable to XSS (script injection), but standard cookies are vulnerable to CSRF. Managing token expiration and secure transmission over WebSockets requires a delicate balance.

**The Win:** Implemented a **Stateless JWT Authentication** system using hardened **HttpOnly Cookies**.

- **Double-Layer Security:** Configured cookies with `HttpOnly` (blocking JS access) and `SameSite=Lax` to mitigate Cross-Site Request Forgery.
- **WSS Integration:** Since WebSockets don't support custom headers in the browser's native API, I leveraged the fact that browsers automatically send cookies during the initial HTTP upgrade handshake.
- **Claims-Based Identity:** Used signed JWT claims to pass the `UserID` directly into the `Client` struct, eliminating the need for a database lookup on every message broadcast.

---

## Challenge 9: The Perimeter Guard (CORS & Origin Validation)

**Problem Statement:** Prevent "Cross-Site WebSocket Hijacking" (CSWH) where a malicious site tries to initiate a WebSocket connection to your server using a victim's active session.

**The Struggle:** Unlike standard REST APIs, WebSockets are not restricted by the "Same-Origin Policy" (SOP). The server must manually verify where the connection request is coming from.

**The Win:** Engineered a **Strict Origin-Validator Middleware** for the WebSocket upgrader.

- **Dynamic Allow-Listing:** Implemented a configurable CORS middleware that validates the `Origin` header against a list of trusted frontend domains.
- **Pre-Upgrade Rejection:** Positioned the origin check inside the `CheckOrigin` function of the WebSocket Upgrader. This shuts down malicious handshakes before the connection is even upgraded.
- **Credential Support:** Explicitly enabled `Access-Control-Allow-Credentials` to allow the secure exchange of JWT cookies between the frontend and backend.

---

## Challenge 10: The Cryptographic Tunnel (WSS & Local CA)

**Problem Statement:** Enable end-to-end encryption for local development to ensure "Secure Context" browser features work and to prevent data sniffing.

**The Struggle:** Modern browsers block "Secure" cookies and certain WebSocket features if the connection is plain `ws://`. However, managing self-signed certificates usually results in annoying "Not Secure" browser warnings.

**The Win:** Orchestrated a **Local Trusted Infrastructure** using a private Certificate Authority (CA).

- **Automated Trust:** Utilized `mkcert` to generate a locally-trusted CA, allowing the Go server to serve `https://localhost` with a valid "Green Padlock" in the browser.
- **Protocol Upgrade:** Switched the server from `http.ListenAndServe` to `http.ListenAndServeTLS`, upgrading all communication from `ws://` to **`wss://`**.
- **Production Parity:** By developing over TLS locally, I ensured that the `Secure: true` cookie flag works exactly as it would in production, catching "Mixed Content" bugs before deployment.

---

## Challenge 11: Multi-Tenancy (Room-based Isolation)

**Problem Statement:** Transform a single-stream "Global Chat" into a scalable multi-room architecture where users are isolated by context and security boundaries.

**The Struggle:** Managing state across multiple rooms creates high risk for "Message Leaking" (users seeing data from rooms they aren't in) and "Zombie Goroutines" (connections staying alive in memory after a room is empty). Additionally, trust-based room assignment allowed malicious users to spoof their RoomID in JSON payloads to spy on other rooms.

**The Win:** Implemented a **Sharded Hub Architecture** with strict server-side validation.

- **Room Sharding:** Re-engineered the `Hub` to manage a map of maps (`map[string]map[*Client]bool`), allowing O(1) lookups for room-specific broadcasting while ensuring memory is reclaimed when a room becomes empty.
- **Identity & Room Enforcement:** Hardened the `ReadPump` to ignore client-provided RoomIDs. The server now forces every message into the `RoomID` associated with the authenticated session, preventing Cross-Room Spoofing.
- **Stateful History Replay:** Integrated a localized history buffer per room. New participants automatically receive a "Context Snapshot" of the last 20 messages upon joining, providing immediate conversation continuity.
- **Direct System Messaging:** Developed a non-blocking notification system that bypasses the global broadcast channel for "User Joined" events, eliminating the risk of internal deadlocks during high-concurrency connection spikes.

---

## Challenge 12: Persistent Storage & Async Write-Ahead Logging

**Problem Statement:** Transition the chat engine from a volatile, memory-only state to a durable, database-backed architecture capable of surviving server restarts and providing infinite history.

**The Struggle:** Writing to a database (PostgreSQL) is significantly slower than RAM. Synchronous database writes during a broadcast would create a "Head-of-Line Blocking" disaster, where one slow disk write freezes the entire chat for all users. Furthermore, a sudden server shutdown could lead to "Data Siloing," where messages sitting in memory buffers are lost forever because the database connection was severed too early.

**The Win:** Engineered a **Non-Blocking Persistence Pipeline** using a Worker-Queue pattern.

- **Asynchronous Persistence Queue:** Decoupled the live broadcast from the storage layer using a buffered channel (`PersistenceQueue`). The Hub "fires and forgets" messages to a background worker, maintaining sub-millisecond latency for active users regardless of database load.
- **SQL-Based Privacy Filtering:** Replaced in-memory history slices with a Postgres Repository. The `Fetch` logic was hardened with a relational "Privacy Shield" query, ensuring that private messages are only replayed to the specific sender or target during the room join phase.
- **Durable History Replay:** Shifted from a fixed 20-message RAM buffer to a "Deep History" fetch. New participants now pull a context snapshot directly from Postgres, allowing for infinite scrolling and historical data retrieval that survives Hub restarts.
- **Graceful Shutdown Orchestration:** Implemented a sophisticated shutdown sequence using `sync.WaitGroup`. The system now ensures the persistence worker "drains" its entire buffer to the database before the application context or database pool is allowed to close, guaranteeing zero message loss during deployments.

---

## Challenge 13: Scalable Acknowledgment Handshaking & State Persistence

**Problem Statement:** Transition from a "Send-and-Pray" delivery model to a verifiable, multi-stage Message Lifecycle. The system needed to support delivery and read receipts (Status Ticks) without creating a "Broadcast Storm" that would choke the server in high-traffic rooms.

**The Struggle:** Traditional real-time updates for every "Read" event create an exponential increase in network traffic ($O(N^2)$). In a room with 100 users, broadcasting a "Seen" event to everyone for a single message creates 99 unnecessary packets. Additionally, out-of-order network delivery (Race Conditions) threatened to overwrite "Seen" statuses with "Delivered" statuses if an older packet arrived late, causing "flickering" UI ticks.

**The Win:** Implemented an **Eventually Consistent Acknowledgment Pipeline** and a **Monotonic State Shield**.

- **Client-Side UUID Sovereignty:** Shifted ID generation from the Database to the Frontend. This allows the Sender and Recipient to reference the exact same message fingerprint instantly, enabling "Optimistic UI" updates and immediate acknowledgment handshaking before the server even confirms the write.
- **Scalable "Silent" Updates:** Engineered a one-way event flow for `TypeAck` messages. Acknowledgments are routed directly from the Hub to the Persistence Worker for a "Database-Only" update, bypassing the broadcast loop. This preserves CPU and bandwidth, relying on "Fetch-on-Refresh" or "Delta-Syncs" for state consistency.
- **Monotonic "Regression Shield" Query:** Hardened the Repository with a state-aware SQL update logic: `WHERE id = $1 AND status < $2`. This ensures the message lifecycle only moves forward (Saved → Delivered → Seen). If a "Delivered" packet arrives after a "Seen" packet due to network jitter, the database safely ignores the "downgrade."
- **Polymorphic Worker Logic:** Upgraded the `PersistMessageWorker` to handle multiple operation types from a single unified stream. The worker now dynamically switches between `INSERT` (for new chat/private messages) and `UPDATE` (for acknowledgments), maintaining strict sequential integrity for every message's lifecycle.
- **Idempotent Storage:** Integrated `ON CONFLICT (id) DO NOTHING` into the persistence layer. This protects the system against duplicate message delivery caused by client-side retries or network "ghosting," ensuring the chat history remains clean and unique.

---

## Challenge 14: Real-Time Presence & Intent Signaling (Typing Indicators)

**Problem Statement:** Enhance the user experience by communicating user intent (Typing...) without compromising the performance of the core persistence engine.

**The Struggle:** High-frequency events like typing can generate dozens of packets per second per user. Treating these as standard messages would result in "Database Noise," where transient UI states clutter the primary chat tables, and "Broadcast Congestion," where the server wastes resources sending a user's own typing status back to them.

**The Win:** Engineered a **Volatile Event Bypass** within the Hub.

- **Ephemeral Routing:** Implemented a non-persistent branch in the Hub's broadcast logic. `TypeTyping` events are identified and routed through the WebSocket layer for immediate delivery but are strictly filtered out of the `PersistenceQueue`, ensuring 0% database overhead for transient states.
- **Echo-Suppression Logic:** Optimized room broadcasts by implementing a sender-identity check. The Hub now intelligently skips the sender’s own connection during a typing-state broadcast, reducing outgoing bandwidth and client-side processing.
- **State-Separation Architecture:** Formally decoupled "Durable Data" (Chat History) from "Transient State" (Presence/Typing). This architectural split prepares the system for Horizontal Scaling, where transient states can eventually be offloaded to an in-memory store like Redis.

---

## Challenge 15: Distributed Scaling & Cluster Synchronization

**Problem Statement:** Transitioning from a single-node server to a distributed cluster while maintaining global session integrity and message consistency.

**The Struggle:** Traditional WebSockets are stateful and tied to a single memory space. Moving to a cluster creates **"State Fragmentation,"** where a user on _Server A_ cannot see a user on _Server B_, and **"Session Ghosting,"** where a user might be logged into multiple instances simultaneously without the system knowing.

**The Win:** Engineered a **Redis-Backed Event Mesh** to synchronize the cluster state.

- **Distributed Session Enforcement:** Implemented a global **"Kick Signal"** using Redis Pub/Sub. When a user registers on any node, the Hub broadcasts a `TypeKick` event with a unique `ServerID` tag. Remote nodes identify and terminate conflicting sessions, while the originating node intelligently ignores the signal to prevent self-disconnection.
- **Pub/Sub Echo Architecture:** Re-engineered the message pipeline to use a **"Redis-First"** delivery model. Messages are published to Redis room-channels and ingested via a background `ListenToRedis` worker. This ensures that message order and "System Join" notifications are perfectly synchronized across all nodes in the cluster.

- **Recursive Deadlock Prevention:** Optimized the Hub's concurrency model by implementing a **"Lock-Split"** registration flow. By decoupling session cleanup from state registration, the system handles rapid re-connections and cross-node handoffs without triggering mutex deadlocks or freezing the broadcast loop.
- **Atomic History Replay:** Synchronized the transition from **"Static History"** (Database) to **"Live Stream"** (Redis) by implementing a calibrated background goroutine for history fetching. This eliminates the race condition where users might miss live messages during the millisecond gap of a room subscription.

---

## Challenge 16: Intelligent Routing & Self-Healing Ring

**Problem Statement:** Solving the **"Blind Redirect"** problem and ensuring cluster resilience when nodes fail without notice (**Ghost Servers**).

**The Struggle:** In a distributed environment, a Load Balancer often sends users to random nodes. Without a routing strategy, users become scattered, forcing massive Redis overhead. Furthermore, if a server crashes, the cluster remains **"blind,"** continuing to route users to a dead IP address, leading to a total breakdown of the connection flow.

**The Win:** Engineered a **Consistent Hashing Circular Buffer** with an **Active Watcher** mechanism.

- **Deterministic User Mapping:** Implemented a **Consistent Hashing Ring** with **VNode (Virtual Node)** support. By mapping `UserID` to a specific point on a $360^\circ$ hash circle, the system ensures that a user is always routed to the same primary node. This drastically reduces "cross-talk" and maximizes local memory efficiency.
- **Automated "Bouncer" Logic:** Developed an intelligent WebSocket middleware that intercepts connections **before** the `HTTP Upgrade`. If the Ring determines a user belongs on a peer node, the server issues a **307 Temporary Redirect** with a `from` tracking parameter, guiding the client to the correct destination without manual intervention.
- **Ghost Server Pruning (The Watcher):** Solved the **"Sudden Death"** scenario by implementing a **TTL-based Heartbeat Ledger**. Each node broadcasts a `JOIN` signal every **5 seconds**. A background **Watcher** goroutine monitors the `LastSeen` ledger; if a peer remains silent for **>20 seconds**, it is atomically purged from the Ring, and its traffic is instantly redistributed to healthy neighbors.
- **Redirect Loop Shield:** Neutralized **"Ping-Pong"** race conditions during cluster re-syncs. By analyzing the `from` query parameter, the server detects if a user is being bounced between nodes with conflicting Ring states. In these edge cases, the server **breaks the cycle** by accepting the connection locally, ensuring 100% uptime during node transitions.

---

## ⚡ Technical Stack

| Category        | Technology                                      |
| :-------------- | :---------------------------------------------- |
| **Language**    | Go (Golang)                                     |
| **Concurrency** | CSP, Goroutines, Channels                       |
| **Protocol**    | WebSockets (RFC 6455)                           |
| **Patterns**    | Fan-out, Non-blocking I/O, Graceful Degradation |

---

## 🧠 Lessons Learned

### 1. Distributed Load vs. Centralized Bottlenecks

**The Insight:** Parsing commands at the "edge" (`ReadPump`) is infinitely more scalable than parsing at the "center" (`Hub`).

- **Why it matters:** By offloading CPU-intensive tasks like regex matching, string manipulation, and JSON unmarshaling to the individual client goroutines, the Hub remains a lean, high-speed traffic controller. This prevents a single "spammy" user from lagging the entire server.

### 2. State as a Single Source of Truth

**The Insight:** You must implement aggressive server-side evictions.

- **Why it matters:** If the server's internal state (the Room map) thinks a client is connected when they are actually "ghosting" (due to a silent network drop), the system is fundamentally broken. We learned that the server must be the final arbiter of truth, using active heartbeats to prune its own state.

### 3. Optimistic UI vs. Backend "Watchdog"

**The Insight:** The UI should react instantly to browser `offline` events, but the backend is the ultimate authority.

- **Why it matters:** We learned to separate the "User Experience" (showing a disconnected icon) from the "Connection Health" (the `ReadDeadline`). The backend doesn't care about the UI; it only cares about the bytes. If the heartbeat fails, the backend watchdog must tear down the connection to maintain data integrity.

### 4. Zero-Trust Identity

**The Insight:** Use JWTs in cookies, but treat them as "Blind Tokens" on the frontend.

- **Why it matters:** By using `HttpOnly` and `Secure` flags, the frontend doesn't need to "read" the token—it only needs to "possess" it. This eliminates XSS-based token theft. The backend remains the only entity that verifies identity, keeping the client-side logic decoupled and secure.

### 5. Explicit Origin Sovereignty

**The Insight:** WebSockets are the "Wild West" of the Same-Origin Policy (SOP).

- **Why it matters:** Unlike REST APIs, WebSockets don't automatically follow browser origin rules. We learned that a secure server must be its own bouncer, explicitly checking the `Origin` header during the HTTP Upgrade. Without this, Cross-Site WebSocket Hijacking (CSWSH) could allow malicious sites to impersonate users.

### 6. Production Parity via Local CA

**The Insight:** Local development must mimic production security constraints early.

- **Why it matters:** Waiting until deployment to test HTTPS/WSS is a recipe for failure. By setting up a local Certificate Authority (`mkcert`), we learned that "Secure Contexts" fundamentally change how browsers handle cookies and TLS handshakes. This ensures the move to production is a configuration change, not a code rewrite.

### 7. Atomic History Pruning & GC Signals

**The Insight:** Slicing in Go is a pointer operation, not a memory deletion.

- **Why it matters:** Slicing a message history (`history[1:]`) keeps the underlying array alive, leading to "Pointer Bloat." We learned to explicitly nil out the deleted index (`history[0] = Message{}`) to signal the Garbage Collector that the memory (especially large strings) can be reclaimed.

### 8. Sequence Consistency & Non-Blocking Registration

**The Insight:** Hub events must be serialized, but goroutine startup must be parallelized.

- **Why it matters:** To prevent deadlocks, we learned to spin up the `ReadPump` and `WritePump` _before_ registering the client with the Hub. This ensures the client is ready to receive the "History Replay" the moment it exists in the Hub's eyes, preventing blocked channels and frozen loops.

### 9. Persistent Path vs. Volatile Path

**The Insight:** Not all data is created equal; treating every packet as "permanent" is a scalability trap.

- **Why it matters:** We learned to bifurcate the Hub’s logic. **Durable data** (Chat/Acks) is routed to the `PersistenceQueue`, while **Transient data** (Typing Indicators) is broadcast and immediately garbage collected. This prevents our database from becoming a graveyard of "User is typing..." garbage.

### 10. The "Regression Shield" (Monotonic State)

**The Insight:** In distributed systems, late-arriving packets are inevitable.

- **Why it matters:** We learned that a simple `UPDATE status = X` is dangerous. By using the SQL condition `WHERE status < new_status`, we created a "Monotonic Shield." This ensures that a "Delivered" receipt arriving after a "Seen" receipt (due to network jitter) cannot move the message status backward in time.

### 11. Decoupling Broadcast from Storage (The Worker-Queue Pattern)

**The Insight:** Database latency must never dictate UI responsiveness.

- **Why it matters:** Writing to Postgres is magnitudes slower than sending a WebSocket frame. By implementing an asynchronous `PersistenceQueue`, we decoupled the "Live Experience" from the "Storage Requirement." The Hub "fires and forgets" to a background worker, ensuring the chat stays snappy even if the database is under heavy load.

### 12. Client-Side ID Sovereignty

**The Insight:** Generating IDs on the server creates an "Identity Gap" during the round-trip.

- **Why it matters:** By shifting UUID generation to the Frontend, the client can "own" the message's identity before the server even acknowledges it. This allows for **Optimistic UI** (showing the message instantly) and provides a reliable handle for future Acks (Delivered/Seen) without waiting for a database-generated sequence number.

### 13. Idempotency & Conflict Resolution

**The Insight:** Network retries will eventually cause duplicate "Insert" attempts.

- **Why it matters:** Since the Frontend now generates IDs, a user clicking "Send" twice or a client-side retry logic could send the same message twice. We learned to use `ON CONFLICT (id) DO NOTHING` to make our `Save` operation **idempotent**, ensuring our chat history remains a "Single Version of Truth" regardless of network flakiness.

### 14. Echo Suppression & Bandwidth Conservation

**The Insight:** Sending a user's own data back to them is a "Broadcasting Tax."

- **Why it matters:** Especially for high-frequency events like **Typing Indicators**, we learned to implement a sender-check in the Hub. By skipping the sender's own connection during the broadcast loop, we halved the outgoing traffic for those events and reduced the client-side CPU overhead of re-processing their own actions.

### 15. Graceful Shutdown & The "Draining" Pattern

**The Insight:** A `kill -9` is a message's worst enemy.

- **Why it matters:** We learned that the persistence worker must be the last thing to die. Using `sync.WaitGroup`, we orchestrated a shutdown sequence where the Hub closes the queue, the worker "drains" every remaining message into the database, and _only then_ does the application exit. This guarantees **Zero Data Loss** during deployments.

---

## 🗺️ The Road Ahead (The Path to 20)

### Phase 1: Core Reliability & Performance

- [x] **Challenge 1:** Core Foundation (Handshake & Handlers).
- [x] **Challenge 2:** Centralized Hub Pattern (State Management).
- [x] **Challenge 3:** Non-blocking Backpressure (Slow Consumer Protection).
- [x] **Challenge 4:** Scalable Command Parsing (Distributed Edge Logic).
- [x] **Challenge 5:** Presence & Node Visualization (State Synchronization).
- [x] **Challenge 6:** The "Bulletproof" Sprint (Anti-Zombie Logic & A11y).

### Phase 2: Security & Traffic Control

- [x] **Challenge 7:** Leaky Bucket Rate Limiting (DDoS & Spam Protection).
- [x] **Challenge 8:** Authentication & JWT Integration (Secure Handshaking).
- [x] **Challenge 9:** CORS & Origin Validation (Cross-Site Security).
- [x] **Challenge 10:** TLS/SSL Integration (WSS Implementation).

### Phase 3: Advanced Messaging Logic

- [x] **Challenge 11:** Multi-Tenancy (Room-based Isolation).
- [x] **Challenge 12:** Message Persistence (Redis/PostgreSQL Integration).
- [x] **Challenge 13:** "Message Delivered" & "Seen" Receipts (Acknowledge Logic).
- [x] **Challenge 14:** Real-Time Presence & Intent Signaling.

### Phase 4: Horizontal Scaling & Distribution

- [x] **Challenge 15:** Distributed Pub/Sub (Redis Integration for Multi-Node).
- [x] **Challenge 16:** Consistent Hashing (Sticky Session Management).
- [ ] **Challenge 17:** Prometheus & Grafana Monitoring (Observability).
- [ ] **Challenge 18:** Dockerization & K8s Readiness (Containerization).

### Phase 5: The Full-Stack Evolution

- [ ] **Challenge 19:** Frontend Migration—Evaluating **Next.js** vs **HTMX + Templ**.
- [ ] **Challenge 20:** Final Deployment—Blue/Green Deployment on Cloud.

---

### How to Run

1. `go run main.go`
2. Open `index.html` in multiple tabs.
3. Toggle "Offline" in Chrome DevTools to test the **Watchdog** and **Anti-Zombie** logic.
