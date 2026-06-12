package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"time"
)

func AddTableToUser(userID uint, tableID string) {
	key := fmt.Sprintf("user:%d", userID)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return
	}
	var user models.UserRedis
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		return
	}
	for _, id := range user.PlayingTableIDs {
		if id == tableID {
			return
		}
	}
	user.PlayingTableIDs = append(user.PlayingTableIDs, tableID)
	updated, _ := json.Marshal(user)
	database.RedisClient.Set(database.Ctx, key, updated, 72*time.Hour)
}

func RemoveTableFromUser(userID uint, tableID string) {
	key := fmt.Sprintf("user:%d", userID)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return
	}
	var user models.UserRedis
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		return
	}
	newIDs := []string{}
	for _, id := range user.PlayingTableIDs {
		if id != tableID {
			newIDs = append(newIDs, id)
		}
	}
	user.PlayingTableIDs = newIDs
	updatedJSON, err := json.Marshal(user)
	if err != nil {
		return
	}
	database.RedisClient.Set(database.Ctx, key, updatedJSON, 72*time.Hour)
}
