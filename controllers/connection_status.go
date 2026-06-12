// controllers/connection_status.go
package controllers

import (
	"encoding/json"
	"lekatika-server/database"
	"lekatika-server/models"
	"time"
)

func MarkUserConnected(userID uint) {
	tables, err := GetTablesForUser(userID)
	if err != nil {
		return
	}
	for _, tableID := range tables {
		key := "table:" + tableID
		val, err := database.RedisClient.Get(database.Ctx, key).Result()
		if err != nil {
			continue
		}
		var table models.PlayingTable
		if err := json.Unmarshal([]byte(val), &table); err != nil {
			continue
		}
		for i, seat := range table.Seats {
			if seat.UserID == int(userID) {
				if i < len(table.SeatsConnected) {
					table.SeatsConnected[i] = true
					table.DisconnectedAt[i] = 0
				}
				break
			}
		}
		updatedJSON, _ := json.Marshal(table)
		database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
		database.PublishTableUpdate(tableID)
	}
}

func MarkUserDisconnected(userID uint) {
	tables, err := GetTablesForUser(userID)
	if err != nil {
		return
	}
	now := time.Now().Unix()
	for _, tableID := range tables {
		key := "table:" + tableID
		val, err := database.RedisClient.Get(database.Ctx, key).Result()
		if err != nil {
			continue
		}
		var table models.PlayingTable
		if err := json.Unmarshal([]byte(val), &table); err != nil {
			continue
		}
		for i, seat := range table.Seats {
			if seat.UserID == int(userID) {
				if i < len(table.SeatsConnected) {
					table.SeatsConnected[i] = false
					table.DisconnectedAt[i] = now
				}
				break
			}
		}
		updatedJSON, _ := json.Marshal(table)
		database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
		database.PublishTableUpdate(tableID)
	}
}
