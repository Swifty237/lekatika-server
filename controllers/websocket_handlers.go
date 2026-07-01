package controllers

import (
	"encoding/json"
	"lekatika-server/database"
	"lekatika-server/models"
	"time"
)

// HandlePlayCard déplace une carte de la main vers les cartes jouées
func HandlePlayCard(tableID string, seatIndex int, cardIndex int, userID uint) {
	key := "table:" + tableID
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return
	}
	// Vérifier que le siège est occupé par l'utilisateur
	if seatIndex >= len(table.Seats) || table.Seats[seatIndex].UserID != int(userID) {
		return
	}
	// Vérifier que la carte existe
	if cardIndex < 0 || cardIndex >= len(table.SeatCards[seatIndex].Hand) {
		return
	}
	// Déplacer la carte
	card := table.SeatCards[seatIndex].Hand[cardIndex]
	table.SeatCards[seatIndex].Hand = append(table.SeatCards[seatIndex].Hand[:cardIndex], table.SeatCards[seatIndex].Hand[cardIndex+1:]...)
	table.SeatCards[seatIndex].Played = append(table.SeatCards[seatIndex].Played, card)
	// Sauvegarder
	updatedJSON, _ := json.Marshal(table)
	database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
	// Notifier tous les clients
	database.PublishTableUpdate(tableID)
}
