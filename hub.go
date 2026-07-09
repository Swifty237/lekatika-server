package main

import (
	"encoding/json"
	"log"
	"sync"

	"lekatika-server/controllers"
	"lekatika-server/database" // ← Import du package database pour Redis

	"github.com/gorilla/websocket"
)

type Client struct {
	conn   *websocket.Conn
	send   chan []byte
	userID uint // ID de l'utilisateur
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
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
		controllers.MarkUserDisconnected(c.userID)
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
