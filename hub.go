package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"lekatika-server/controllers"
	"lekatika-server/database" // ← Import du package database pour Redis
	"lekatika-server/models"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn   *websocket.Conn
	send   chan []byte
	userID uint // ID de l'utilisateur
}

type TimerInfo struct {
	SeatIndex int
	Ticker    *time.Ticker
	StopChan  chan struct{}
	Remaining int
	mu        sync.Mutex
}

type Hub struct {
	clients     map[*Client]bool
	broadcast   chan []byte
	register    chan *Client
	unregister  chan *Client
	mu          sync.RWMutex
	tableTimers map[string]*TimerInfo
	timersMu    sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		clients:     make(map[*Client]bool),
		broadcast:   make(chan []byte),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		tableTimers: make(map[string]*TimerInfo),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			go h.broadcastOnlineUsers()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			go h.broadcastOnlineUsers()
		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) StartTimer(tableID string, seatIndex int) {
	h.timersMu.Lock()
	defer h.timersMu.Unlock()

	// Arrêter l'ancien timer
	if info, ok := h.tableTimers[tableID]; ok {
		info.Ticker.Stop()
		close(info.StopChan)
		delete(h.tableTimers, tableID)
	}

	ticker := time.NewTicker(1 * time.Second)
	stopChan := make(chan struct{})
	info := &TimerInfo{
		SeatIndex: seatIndex,
		Ticker:    ticker,
		StopChan:  stopChan,
		Remaining: 30,
	}
	h.tableTimers[tableID] = info

	h.broadcastTimerEvent(tableID, "TIMER_START", seatIndex, info.Remaining)

	go func() {
		for {
			select {
			case <-ticker.C:
				info.mu.Lock()
				info.Remaining--
				remaining := info.Remaining
				info.mu.Unlock()

				if remaining <= 0 {
					h.broadcastTimerEvent(tableID, "TIMER_END", seatIndex, 0)
					h.timersMu.Lock()
					if info, ok := h.tableTimers[tableID]; ok {
						info.Ticker.Stop()
						close(info.StopChan)
						delete(h.tableTimers, tableID)
					}
					h.timersMu.Unlock()
					h.handleTimerExpired(tableID, seatIndex)
					return
				}
				h.broadcastTimerEvent(tableID, "TIMER_TICK", seatIndex, remaining)
			case <-stopChan:
				return
			}
		}
	}()
}

func (h *Hub) StopTimer(tableID string) {
	h.timersMu.Lock()
	defer h.timersMu.Unlock()
	if info, ok := h.tableTimers[tableID]; ok {
		info.Ticker.Stop()
		close(info.StopChan)
		delete(h.tableTimers, tableID)
	}
}

func (h *Hub) broadcastTimerEvent(tableID string, eventType string, seatIndex int, remaining int) {
	payload := map[string]interface{}{
		"type":      eventType,
		"tableId":   tableID,
		"seatIndex": seatIndex,
		"remaining": remaining,
	}
	data, _ := json.Marshal(payload)
	h.broadcast <- data
}

func (h *Hub) handleTimerExpired(tableID string, seatIndex int) {
	// Récupérer la table et déclencher l'auto-play
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return
	}
	if seatIndex < 0 || seatIndex >= len(table.Seats) {
		return
	}
	userID := uint(table.Seats[seatIndex].UserID)
	if userID == 0 {
		return
	}
	// Mettre en pause
	controllers.HandleToggleBreak(tableID, seatIndex, userID)
	// Auto-play
	controllers.AutoPlay(tableID, seatIndex)
}

func (h *Hub) GetTimerState(tableID string) (active bool, seatIndex int, remaining int) {
	h.timersMu.Lock()
	defer h.timersMu.Unlock()
	info, ok := h.tableTimers[tableID]
	if !ok {
		return false, -1, 0
	}
	info.mu.Lock()
	remaining = info.Remaining
	seatIndex = info.SeatIndex
	info.mu.Unlock()
	return true, seatIndex, remaining
}

// subscribeToRedis s'abonne au canal Redis "tables" et diffuse les messages reçus à tous les clients WebSocket
func (h *Hub) subscribeToRedis() {
	pubsub := database.RedisClient.Subscribe(database.Ctx, "tables")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		h.broadcast <- []byte(msg.Payload)
	}
}

// writePump envoie les messages du canal send vers la connexion WebSocket
func (c *Client) writePump() {
	for msg := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			break
		}
	}
	c.conn.Close()
}

// readPump lit les messages de la connexion WebSocket et les traite
func (c *Client) readPump(hub *Hub) {
	defer func() {
		controllers.MarkUserDisconnectedToTable(c.userID)
		hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		// Décoder le message JSON
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}
		msgType, ok := msg["type"].(string)
		if !ok {
			continue
		}
		switch msgType {
		case "PLAY_CARD":
			tableID, _ := msg["tableId"].(string)
			seatIdxFloat, _ := msg["seatIndex"].(float64)
			cardIdxFloat, _ := msg["cardIndex"].(float64)
			if tableID != "" {
				controllers.HandlePlayCard(tableID, int(seatIdxFloat), int(cardIdxFloat), c.userID)
			}

		case "CHAT_MESSAGE":
			tableID, _ := msg["tableId"].(string)
			content, _ := msg["content"].(string)
			if tableID != "" && content != "" {
				controllers.HandleChatMessage(tableID, c.userID, content)
			}

		case "TOGGLE_BREAK":
			tableID, _ := msg["tableId"].(string)
			seatIdxFloat, _ := msg["seatIndex"].(float64)
			seatIndex := int(seatIdxFloat)
			if tableID != "" {
				controllers.HandleToggleBreak(tableID, seatIndex, c.userID)
			}

		case "CHECK_SQUARE":
			tableID, _ := msg["tableId"].(string)
			seatIdxFloat, _ := msg["seatIndex"].(float64)
			seatIndex := int(seatIdxFloat)
			value := 0
			if v, ok := msg["value"].(float64); ok {
				value = int(v)
			}
			log.Printf("CHECK_SQUARE reçu: table=%s, seat=%d, user=%d, value=%d", tableID, seatIndex, c.userID, value)
			if tableID != "" {
				err := controllers.AddAnnouncement(tableID, seatIndex, c.userID, "square", value)
				if err != nil {
					log.Printf("Erreur lors de l'ajout de l'annonce: %v", err)
					// Envoyer un message d'erreur privé au client
					errorPayload := map[string]interface{}{
						"type":    "ERROR",
						"message": err.Error(),
					}
					hub.SendPrivateMessage(c.userID, errorPayload)
				}
			}

		case "CHECK_THREE_SEVEN":
			tableID, _ := msg["tableId"].(string)
			seatIdxFloat, _ := msg["seatIndex"].(float64)
			seatIndex := int(seatIdxFloat)
			value := 0
			if v, ok := msg["value"].(float64); ok {
				value = int(v)
			}
			log.Printf("CHECK_THREE_SEVEN reçu: table=%s, seat=%d, user=%d, value=%d", tableID, seatIndex, c.userID, value)
			if tableID != "" {
				err := controllers.AddAnnouncement(tableID, seatIndex, c.userID, "three_seven", value)
				if err != nil {
					log.Printf("Erreur lors de l'ajout de l'annonce: %v", err)
					errorPayload := map[string]interface{}{
						"type":    "ERROR",
						"message": err.Error(),
					}
					hub.SendPrivateMessage(c.userID, errorPayload)
				}
			}

		case "CHECK_TIA":
			tableID, _ := msg["tableId"].(string)
			seatIdxFloat, _ := msg["seatIndex"].(float64)
			seatIndex := int(seatIdxFloat)
			value := 0
			if v, ok := msg["value"].(float64); ok {
				value = int(v)
			}
			log.Printf("CHECK_TIA reçu: table=%s, seat=%d, user=%d, value=%d", tableID, seatIndex, c.userID, value)
			if tableID != "" {
				err := controllers.AddAnnouncement(tableID, seatIndex, c.userID, "tia", value)
				if err != nil {
					log.Printf("Erreur lors de l'ajout de l'annonce: %v", err)
					errorPayload := map[string]interface{}{
						"type":    "ERROR",
						"message": err.Error(),
					}
					hub.SendPrivateMessage(c.userID, errorPayload)
				}
			}
		}
	}
}

// SendPrivateMessage envoie un message à un utilisateur spécifique par son ID
func (h *Hub) SendPrivateMessage(userID uint, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	found := false
	for client := range h.clients {
		if client.userID == userID {
			select {
			case client.send <- data:
				found = true
			default:
				log.Printf("Client %d saturé, message non envoyé", userID)
			}
			break
		}
	}
	if !found {
		log.Printf("Message privé pour l'utilisateur %d non délivré : client non trouvé dans le hub", userID)
	}
}

func (h *Hub) GetClientsCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) GetOnlineUsersList() []models.UserRedis {
	h.mu.RLock()
	defer h.mu.RUnlock()

	seen := make(map[uint]bool)
	var users []models.UserRedis

	for client := range h.clients {
		if seen[client.userID] {
			continue
		}
		seen[client.userID] = true

		user, err := controllers.GetUserByIDFromRedis(client.userID)
		if err == nil {
			users = append(users, user)
		}
	}
	return users
}

func (h *Hub) broadcastOnlineUsers() {
	users := h.GetOnlineUsersList()
	payload := map[string]interface{}{
		"type":  "ONLINE_USERS_UPDATE",
		"users": users,
	}
	data, _ := json.Marshal(payload)
	h.broadcast <- data
}
