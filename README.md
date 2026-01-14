# 🚀 High-Concurrence WebSocket Engine

A high-performance, thread-safe real-time communication server built with **Go** and **Gorilla WebSockets**. This project demonstrates advanced systems engineering patterns, including non-blocking backpressure, graceful process termination, and distributed command parsing.

---

## 🏗️ System Architecture

The server utilizes a **Centralized Hub Pattern** with a **Fan-out** mechanism to manage state and message distribution across multiple concurrent goroutines.

---

## 🛠️ Engineering Challenges & Roadmap

I built this system through a series of six progressive architectural challenges, focusing on concurrency, safety, and reliability:

### Challenge 1: The Hub-and-Spoke Concurrency Model

- **Objective:** Manage state across hundreds of simultaneous connections without causing data races.
- **Implementation:** Designed a central `Hub` that manages client state via Go channels. This ensures that the `clients` map is only ever modified by a single goroutine, eliminating the need for complex mutex locking and avoiding race conditions.

### Challenge 2: Bidirectional Data Flow (Pumps)

- **Objective:** Decouple reading from writing to prevent network I/O from blocking application logic.
- **Implementation:** Designed `readPump` and `writePump` goroutines for every client. This allows the server to process incoming messages and outgoing heartbeats (pings/pongs) independently, maximizing throughput.

### Challenge 3: Non-Blocking Backpressure (The Deadlock Guard)

- **Objective:** Prevent a "Slow Consumer" from filling its buffer and causing the entire broadcast loop to hang.
- **Implementation:** Utilized a `select` statement with a `default` case during broadcasting. If a client's buffer is full, the server drops the client asynchronously to protect the system's overall latency and prevent a deadlock.

### Challenge 4: Real-Time State Synchronization (User List)

- **Objective:** Maintain a consistent, live view of online users across all connected clients.
- **Solution:** Developed a `TypeUserList` protocol. The Hub broadcasts a serialized JSON snapshot of active users whenever a `register` or `unregister` event occurs, ensuring the UI remains in sync with the backend state.

### Challenge 5: Graceful Process Lifecycle (SIGTERM Handling)

- **Objective:** Prevent orphaned connections and data loss during server restarts or shutdowns.
- **Implementation:** Integrated OS signal monitoring (`SIGTERM`, `os.Interrupt`). Upon receiving a signal, the server initiates a "Drain" sequence, closing the `quit` channel to notify all clients and clean up resources gracefully before exiting.

### Challenge 6: Distributed Content Parsing (Slash Commands)

- **Objective:** Implement a scalable way to modify message content (e.g., `/shrug`) without overloading the Hub.
- **Implementation:** Offloaded parsing logic to the `readPump` level. By using a `switch` statement and `strings.SplitN` at the client level, the CPU load for parsing is distributed across all available cores rather than bottlenecking the central Hub.

---

## ⚡ Technical Stack

| Category        | Technology                                      |
| :-------------- | :---------------------------------------------- |
| **Language**    | Go (Golang)                                     |
| **Concurrency** | CSP (Communicating Sequential Processes)        |
| **Protocol**    | WebSockets (RFC 6455)                           |
| **Patterns**    | Fan-out, Non-blocking I/O, Graceful Degradation |

---
