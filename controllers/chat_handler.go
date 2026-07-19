package controllers

import (
	"encoding/json"
	"lekatika-server/database"
	"lekatika-server/models"
	"time"

	"github.com/google/uuid"
)

func HandleChatMessage(tableID string, userID uint, content string) {
	// Récupérer la table
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return
	}

	// Créer le message
	msg := models.ChatMessage{
		ID:        uuid.New().String(),
		UserID:    userID,
		Username:  GetUsernameByUserID(userID),
		Content:   content,
		Timestamp: time.Now(),
	}
	// Ajouter à la liste
	table.ChatMessages = append(table.ChatMessages, msg)
	// Limiter à 100 messages
	if len(table.ChatMessages) > 100 {
		table.ChatMessages = table.ChatMessages[1:]
	}
	// Sauvegarder la table (persistance)
	SaveAndNotify(&table)

	// Envoyer un événement WebSocket dédié pour ce message
	event := map[string]interface{}{
		"type":    "CHAT_MESSAGE",
		"tableId": tableID,
		"message": msg,
	}
	eventJSON, _ := json.Marshal(event)
	database.RedisClient.Publish(database.Ctx, "tables", eventJSON)
}
