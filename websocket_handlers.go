package main

import (
	"encoding/json"
	"lekatika-server/controllers"
	"lekatika-server/database"
	"lekatika-server/models"
	"log"
	"time"
)

// HandleThreeSeven vérifie la règle des 3 sept
func HandleThreeSeven(hub *Hub, tableID string, seatIndex int, userID uint) {
	log.Printf("HandleThreeSeven: table=%s, seat=%d, user=%d", tableID, seatIndex, userID)
	// Récupérer la table
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		log.Printf("Erreur récupération table pour 3 sept: %v", err)
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		log.Printf("Erreur désérialisation table pour 3 sept: %v", err)
		return
	}

	// Vérifier que le siège est occupé par l'utilisateur
	if seatIndex >= len(table.Seats) || table.Seats[seatIndex].UserID != int(userID) {
		hub.SendPrivateMessage(userID, map[string]interface{}{
			"type":    "GAME_EVENT",
			"message": "Vous n'êtes pas assis sur ce siège",
			"isError": "true",
		})
		return
	}

	hand := table.SeatCards[seatIndex].Hand
	if len(hand) < 4 {
		hub.SendPrivateMessage(userID, map[string]interface{}{
			"type":    "GAME_EVENT",
			"message": "3 sept invalide : vous avez déjà joué trop de cartes",
			"isError": "true",
		})
		return
	}

	sevenCount := 0
	for _, card := range hand {
		if len(card) >= 2 && card[1] == '7' {
			sevenCount++
		}
	}

	if sevenCount >= 3 {
		username := controllers.GetUsernameFromSeat(tableID, seatIndex) // note : il faut exporter cette fonction
		controllers.AwardPotToWinner(&table, seatIndex)                 // exporter aussi
		// Réinitialiser
		table.CurrentRound = 0
		table.SuitRequired = ""
		table.RoundPlayedCards = []models.RoundCard{}
		table.CurrentTurnSeatIndex = -1
		for i := range table.SeatCards {
			table.SeatCards[i].Hand = []string{}
			table.SeatCards[i].Played = []string{}
		}
		controllers.SaveAndNotify(&table)

		database.PublishGameEvent(tableID, username+" gagne la manche avec 3 sept !")

		go func() {
			time.Sleep(3 * time.Second)
			controllers.DistributeCardsForHand(tableID) // exporter aussi
		}()
	} else {

		hub.SendPrivateMessage(userID, map[string]interface{}{
			"type":    "GAME_EVENT",
			"message": "3 sept invalide : vous n'avez pas 3 sept en main",
			"isError": "true",
		})
	}
}
