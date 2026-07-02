package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
)

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
	// Récupérer la carte
	card := table.SeatCards[seatIndex].Hand[cardIndex]
	// Appeler la logique de jeu
	if err := ProcessPlayCard(&table, seatIndex, card); err != nil {
		// En cas d'erreur, on peut envoyer un message d'erreur via WebSocket
		// Pour l'instant, on log
		fmt.Printf("Erreur lors du jeu de la carte: %v\n", err)
		return
	}
	// La table a été mise à jour par ProcessPlayCard, qui appelle saveAndNotify
	// On n'a pas besoin de sauvegarder ici
}
