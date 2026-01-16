package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Configure this properly in production
	},
}

type WebSocketHandler struct {
	server *Server
	hub    *Hub
}

func (ws WebSocketHandler) router(server *Server) {
	ws.server = server
	ws.hub = server.wsHub

	// WebSocket endpoints
	wsGroup := server.router.Group("/api/v1/ws")
	wsGroup.Use(ws.server.authMiddleware.AuthenticatedMiddleware())
	{
		// User chat WebSocket
		wsGroup.GET("/chat", ws.handleUserWebSocket)

		// Admin support WebSocket
		wsGroup.GET("/admin/support", ws.handleAdminWebSocket)
	}
}

type WSMessage struct {
	Type      string         `json:"type"` // message:new, ticket:assigned, ticket:updated, notification:new
	TicketID  int64          `json:"ticket_id,omitempty"`
	Data      any            `json:"data"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Client struct {
	ID         string
	UserID     int64
	UserRole   string
	TicketID   int64 // 0 means not subscribed to specific ticket
	Connection *websocket.Conn
	Send       chan WSMessage
	Hub        *Hub
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan WSMessage
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	logger     *logging.Logger
}

func NewHub(logger *logging.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan WSMessage, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Infof("Client %s registered (UserID: %d)", client.ID, client.UserID)
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()
			h.logger.Infof("Client %s unregistered (UserID: %d)", client.ID, client.UserID)
		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				// route message based on tpe and subscription
				if h.shouldSendToClient(client, message) {
					select {
					case client.Send <- message:
					default:
						// Clients send channel is full, close connection
						h.mu.RUnlock()
						close(client.Send)
						h.mu.Lock()
						delete(h.clients, client)
						h.mu.Unlock()
						h.mu.RLock()
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) shouldSendToClient(client *Client, message WSMessage) bool {
	switch message.Type {
	case "message:new", "ticket.updated":
		// send to user/admin subscribed to this ticket
		return client.TicketID == message.TicketID || (client.UserRole != models.USER && message.TicketID > 0)
	case "ticket:assigned":
		// Send to the assigned admin and the ticket owner
		if metadata, ok := message.Metadata["assigned_to"].(int64); ok {
			return client.UserID == metadata
		}
		if metadata, ok := message.Metadata["user_id"].(int64); ok {
			return client.UserID == metadata
		}
		return false
	case "notification:new":
		// send only to specific user
		if metadata, ok := message.Metadata["user_id"].(int64); ok {
			return client.UserID == metadata
		}
		return false
	default:
		return false
	}
}

func (h *Hub) BroadcastMessage(message WSMessage) {
	message.Timestamp = time.Now()
	h.broadcast <- message
}

// readPump pumps messages from the websocket connection to the hub.
func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Connection.Close()
	}()

	c.Connection.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Connection.SetPongHandler(func(string) error {
		c.Connection.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Connection.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.logger.Error(fmt.Sprintf("WebSocket error: %v", err))
			}
			break
		}

		// Handle incoming messages (e.g., typing indicators, read receipts)
		var incomingMsg map[string]any
		if err := json.Unmarshal(message, &incomingMsg); err != nil {
			c.Hub.logger.Error(fmt.Sprintf("Failed to parse message: %v", err))
			continue
		}

		// Process different types of incoming messages
		msgType, ok := incomingMsg["type"].(string)
		if !ok {
			continue
		}

		switch msgType {
		case "ping":
			// Respond with pong
			pongMsg := WSMessage{
				Type: "pong",
				Data: map[string]any{"status": "alive"},
			}
			c.Send <- pongMsg

		case "typing":
			// Broadcast typing indicator to other clients in the same ticket
			if c.TicketID > 0 {
				typingMsg := WSMessage{
					Type:     "user:typing",
					TicketID: c.TicketID,
					Data: map[string]any{
						"user_id": c.UserID,
						"typing":  true,
					},
				}
				c.Hub.BroadcastMessage(typingMsg)
			}

		case "subscribe":
			// Allow dynamic ticket subscription
			if ticketID, ok := incomingMsg["ticket_id"].(float64); ok {
				c.TicketID = int64(ticketID)
				c.Hub.logger.Info(fmt.Sprintf("Client %s subscribed to ticket %d", c.ID, c.TicketID))
			}
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Connection.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Connection.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			err := c.Connection.WriteJSON(message)
			if err != nil {
				c.Hub.logger.Error(fmt.Sprintf("Failed to write message: %v", err))
				return
			}

		case <-ticker.C:
			c.Connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// WebSocket endpoint handlers

// handleUserWebSocket godoc
// @Summary User WebSocket Connection
// @Description Establish WebSocket connection for real-time chat updates
// @Tags websocket
// @Produce json
// @Security BearerAuth
// @Param ticket_id query string false "Ticket ID to subscribe to"
// @Router /api/v1/ws/chat [get]
func (ws *WebSocketHandler) handleUserWebSocket(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		ws.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		ws.server.logger.WithFields(map[string]any{
			"headers": c.Request.Header,
			"error":   err.Error(),
		}).Error("Failed to upgrade connection")
		return
	}

	ticketIDStr := c.Query("ticket_id")
	ticketID := 0
	if ticketIDStr != "" {
		ticketID, _ = strconv.Atoi(ticketIDStr)
	}

	// Create new client and register it to the hub
	client := &Client{
		ID:         uuid.New().String(),
		UserID:     activeUser.UserID,
		UserRole:   activeUser.Role,
		TicketID:   int64(ticketID),
		Connection: conn,
		Send:       make(chan WSMessage, 256),
		Hub:        ws.hub,
	}

	// Register client to the hub
	ws.hub.register <- client

	// Send welcome message
	welcomeMsg := WSMessage{
		Type: "connection:established",
		Data: map[string]any{
			"client_id": client.ID,
			"user_id":   client.UserID,
			"ticket_id": client.TicketID,
		},
	}
	client.Send <- welcomeMsg

	// Start read and write pumps
	go client.readPump()
	go client.writePump()
}

// handleAdminWebSocket godoc
// @Summary Admin WebSocket Connection
// @Description Establish WebSocket connection for admin notifications and ticket updates
// @Tags websocket
// @Produce json
// @Security BearerAuth
// @Router /api/v1/ws/admin/support [get]
func (ws *WebSocketHandler) handleAdminWebSocket(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ws.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		ws.server.logger.WithFields(map[string]any{
			"headers": ctx.Request.Header,
			"error":   err.Error(),
		}).Error("Failed to upgrade connection (admin)")
		return
	}

	// Create new client and register it to the hub
	client := &Client{
		ID:         uuid.New().String(),
		UserID:     activeUser.UserID,
		UserRole:   activeUser.Role,
		TicketID:   0, // admin recieves all ticket updates
		Connection: conn,
		Send:       make(chan WSMessage, 256),
		Hub:        ws.hub,
	}

	// Register client to the hub
	ws.hub.register <- client

	// Send welcome message
	welcomeMsg := WSMessage{
		Type: "connection:established",
		Data: map[string]any{
			"client_id": client.ID,
			"user_id":   client.UserID,
			"role":      client.UserRole,
		},
	}
	client.Send <- welcomeMsg

	// Start read and write pumps
	go client.readPump()
	go client.writePump()
}

// User connecting to chat js example
// const ws = new WebSocket('wss://api.example.com/api/v1/ws/chat?ticket_id=123');

// ws.onopen = () => {
//     console.log('WebSocket connected');
// };

// ws.onmessage = (event) => {
//     const message = JSON.parse(event.data);

//     switch(message.type) {
//         case 'connection:established':
//             console.log('Connected:', message.data);
//             break;

//         case 'message:new':
//             displayMessage(message.data);
//             break;

//         case 'ticket:assigned':
//             showNotification('A support agent has joined the chat');
//             break;

//         case 'user:typing':
//             showTypingIndicator(message.data.user_id);
//             break;
//     }
// };

// // Send typing indicator
// function onUserTyping() {
//     ws.send(JSON.stringify({
//         type: 'typing',
//         ticket_id: currentTicketId
//     }));
// }

// // Send ping to keep connection alive
// setInterval(() => {
//     ws.send(JSON.stringify({ type: 'ping' }));
// }, 30000);

// Admin connecting to support dashboard
// const adminWs = new WebSocket('wss://api.example.com/api/v1/ws/admin/support');

// adminWs.onmessage = (event) => {
//     const message = JSON.parse(event.data);

//     switch(message.type) {
//         case 'ticket:assigned':
//             if (message.metadata.assigned_to === currentAdminId) {
//                 showNotification('New ticket assigned to you');
//                 refreshTicketList();
//             }
//             break;

//         case 'message:new':
//             updateTicketPreview(message.ticket_id);
//             playNotificationSound();
//             break;
//     }
// };
