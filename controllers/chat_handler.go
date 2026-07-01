package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"time"

	"github.com/google/uuid"
)

func HandleChatMessage(tableID string, userID uint, content string) {
	key := "table:" + tableID
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return
	}

	// Récupérer le nom d'utilisateur depuis Redis
	username := "Joueur"
	userKey := fmt.Sprintf("user:%d", userID)
	userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
	if err == nil {
		var user models.UserRedis
		if err := json.Unmarshal([]byte(userVal), &user); err == nil {
			username = user.Username
		}
	}

	msg := models.ChatMessage{
		ID:        uuid.New().String(), // <-- ID unique
		UserID:    userID,
		Username:  username,
		Content:   content,
		Timestamp: time.Now(),
	}
	table.ChatMessages = append(table.ChatMessages, msg)

	updatedJSON, _ := json.Marshal(table)
	database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
	database.PublishTableUpdate(tableID)
}
