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

## ⚡ Technical Stack

| Category        | Technology                                      |
| :-------------- | :---------------------------------------------- |
| **Language**    | Go (Golang)                                     |
| **Concurrency** | CSP, Goroutines, Channels                       |
| **Protocol**    | WebSockets (RFC 6455)                           |
| **Patterns**    | Fan-out, Non-blocking I/O, Graceful Degradation |

---

## 🧠 Lessons Learned

### 1. **Distributed Load:** Parsing commands at the edge (`readPump`) is infinitely more scalable than parsing at the center (`Hub`).

### 2. **State is a Single Source of Truth:** You must implement server-side evictions. If the server thinks a client is there but they aren't, the system is broken.

### 3. **Optimistic UI vs. Truth:** The UI should react instantly to browser `offline` events, but the backend "Watchdog" is the final arbiter of connection health.

### 4. Zero-Trust Identity (Challenge 8)

**The Insight:** Use JWTs in cookies, but treat them as "Blind Tokens" on the frontend.

- **Why it matters:** By using `HttpOnly` and `Secure` flags, we acknowledge that the frontend doesn't need to "read" the token—it only needs to "possess" it. This eliminates XSS-based token theft. We learned that the backend is the only entity that needs to know the user's true identity, keeping the frontend logic lean and secure.

### 5. Explicit Origin Sovereignty (Challenge 9)

**The Insight:** WebSockets are the "Wild West" of the SOP (Same-Origin Policy).

- **Why it matters:** Unlike standard REST APIs, WebSockets don't automatically follow the browser's origin rules. We learned that a secure server must be its own bouncer, explicitly checking the `Origin` header during the HTTP Upgrade. Without this, any malicious site could "hijack" a user's connection.

### 6. Production Parity via Local CA (Challenge 10)

**The Insight:** Local development must mimic production security constraints early.

- **Why it matters:** Waiting until deployment to test HTTPS/WSS is a recipe for failure. By setting up a local Certificate Authority (mkcert), we learned that "Secure Contexts" change how browsers behave (like cookie handling and API access). Solving these "Green Padlock" issues locally ensures that the move to production is just a configuration change, not a code rewrite.

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

- [ ] **Challenge 11:** Multi-Tenancy (Room-based Isolation).
- [ ] **Challenge 12:** Message Persistence (Redis/PostgreSQL Integration).
- [ ] **Challenge 13:** "Message Delivered" & "Seen" Receipts (Acknowledge Logic).
- [ ] **Challenge 14:** Binary Data Support (File Transfers & Image Previews).

### Phase 4: Horizontal Scaling & Distribution

- [ ] **Challenge 15:** Distributed Pub/Sub (Redis Integration for Multi-Node).
- [ ] **Challenge 16:** Consistent Hashing (Sticky Session Management).
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
