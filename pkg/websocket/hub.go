package websocket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

// Upgrader is the default WebSocket upgrader used by all handlers. In
// production the CheckOrigin function should be tightened to the expected
// origin list.
var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Client represents a single WebSocket connection subscribed to a job.
type Client struct {
	Hub    *Hub
	Conn   *websocket.Conn
	JobID  string
	UserID string
	Send   chan []byte
}

// Hub maintains active WebSocket connections grouped by job ID and
// broadcasts messages to all clients watching a given job.
type Hub struct {
	jobs       map[string]map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	log        *zap.Logger
}

// NewHub creates a Hub. Call Run in a separate goroutine to start
// processing register/unregister events.
func NewHub(log *zap.Logger) *Hub {
	return &Hub{
		jobs:       make(map[string]map[*Client]struct{}),
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
		log:        log,
	}
}

// Run is the hub's main event loop. It must be called in its own goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if _, ok := h.jobs[client.JobID]; !ok {
				h.jobs[client.JobID] = make(map[*Client]struct{})
			}
			h.jobs[client.JobID][client] = struct{}{}
			h.mu.Unlock()

			h.log.Debug("WebSocket client registered",
				zap.String("job_id", client.JobID),
				zap.String("user_id", client.UserID),
			)

		case client := <-h.unregister:
			h.removeClient(client)
			h.log.Debug("WebSocket client unregistered",
				zap.String("job_id", client.JobID),
				zap.String("user_id", client.UserID),
			)
		}
	}
}

// Register enqueues a client for addition to the hub.
func (h *Hub) Register(client *Client) { h.register <- client }

// Unregister enqueues a client for removal from the hub.
func (h *Hub) Unregister(client *Client) { h.unregister <- client }

// SendToJob marshals payload as JSON and delivers it to every client
// watching the given job. Slow or dead clients are evicted.
func (h *Hub) SendToJob(jobID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("websocket: marshal payload: %w", err)
	}

	h.mu.RLock()
	clients, ok := h.jobs[jobID]
	h.mu.RUnlock()

	if !ok || len(clients) == 0 {
		h.log.Warn("No WebSocket clients for job", zap.String("job_id", jobID))
		return nil
	}

	for client := range clients {
		select {
		case client.Send <- data:
		default:
			go h.Unregister(client)
		}
	}
	return nil
}

func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.jobs[client.JobID]; ok {
		if _, exists := clients[client]; exists {
			delete(clients, client)
			close(client.Send)
			if len(clients) == 0 {
				delete(h.jobs, client.JobID)
			}
		}
	}
}

// NewClient creates a Client bound to a hub, connection, job, and user.
func NewClient(hub *Hub, conn *websocket.Conn, jobID, userID string) *Client {
	return &Client{
		Hub:    hub,
		Conn:   conn,
		JobID:  jobID,
		UserID: userID,
		Send:   make(chan []byte, 256),
	}
}

// WritePump transfers messages from the Send channel to the WebSocket
// connection and sends periodic pings. Run in its own goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump reads from the WebSocket connection to keep it alive via pong
// handling. When the connection closes the client is unregistered. Run in
// its own goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.Hub.log.Warn("WebSocket unexpected close",
					zap.String("job_id", c.JobID),
					zap.Error(err),
				)
			}
			break
		}
	}
}
