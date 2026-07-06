package main

import (
	"encoding/json"
	"lekatika-server/controllers"
	"lekatika-server/database"
	"lekatika-server/models"
	"log"
)

// HandleThreeSeven vérifie la règle des 3 sept
func HandleThreeSeven(hub *Hub, tableID string, seatIndex int, userID uint) {
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

	totalSeven := 0
	for _, card := range hand {
		if len(card) >= 2 && card[1] == '7' {
			totalSeven++
		}
	}

	for _, card := range table.SeatCards[seatIndex].Played {
		if len(card) >= 2 && card[1] == '7' {
			totalSeven++
		}
	}

	if totalSeven >= 3 {
		// Enregistrer l'annonce
		controllers.ProcessAnnouncement(tableID, seatIndex, userID, "three_seven", 0)
	} else {
		hub.SendPrivateMessage(userID, map[string]interface{}{
			"type":    "GAME_EVENT",
			"message": "3 sept invalide : vous n'avez pas 3 sept en main ou joués",
			"isError": "true",
		})
	}
}

func HandleTia(hub *Hub, tableID string, seatIndex int, userID uint) {
	// Récupérer la table
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		log.Printf("Erreur récupération table pour Tia: %v", err)
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		log.Printf("Erreur désérialisation table pour Tia: %v", err)
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
	// Vérifier que le joueur a joué au plus une carte (il reste au moins 4 cartes en main)
	if len(hand) < 4 {
		hub.SendPrivateMessage(userID, map[string]interface{}{
			"type":    "GAME_EVENT",
			"message": "Tia invalide : vous avez déjà joué trop de cartes",
			"isError": "true",
		})
		return
	}

	// Calculer la somme des valeurs de toutes les cartes (main + jouées)
	totalValue := 0
	for _, card := range hand {
		totalValue += controllers.CardValue(card) // exporter CardValue si nécessaire
	}

	for _, card := range table.SeatCards[seatIndex].Played {
		totalValue += controllers.CardValue(card)
	}

	if totalValue <= 21 {
		// Enregistrer l'annonce
		controllers.ProcessAnnouncement(tableID, seatIndex, userID, "three_seven", totalValue)
	} else {
		hub.SendPrivateMessage(userID, map[string]interface{}{
			"type":    "GAME_EVENT",
			"message": "Tia invalide : la somme de vos cartes dépasse 21",
			"isError": "true",
		})
	}
}

// HandleSquare vérifie la règle du carré
func HandleSquare(hub *Hub, tableID string, seatIndex int, userID uint) {
	// Récupérer la table
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		log.Printf("Erreur récupération table pour carré: %v", err)
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		log.Printf("Erreur désérialisation table pour carré: %v", err)
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

	// Vérifier qu'il reste assez de cartes (le joueur n'a pas trop joué)
	hand := table.SeatCards[seatIndex].Hand
	if len(hand) < 4 {
		hub.SendPrivateMessage(userID, map[string]interface{}{
			"type":    "GAME_EVENT",
			"message": "Carré invalide : vous avez déjà joué trop de cartes",
			"isError": "true",
		})
		return
	}

	// Compter les occurrences de chaque hauteur (main + déjà jouées)
	heightCount := make(map[int]int)
	for _, card := range hand {
		height := controllers.CardValue(card)
		heightCount[height]++
	}
	for _, card := range table.SeatCards[seatIndex].Played {
		height := controllers.CardValue(card)
		heightCount[height]++
	}

	// Vérifier s'il y a une hauteur avec au moins 4 occurrences
	for height, count := range heightCount {
		if count >= 4 {
			// Carré valide : on enregistre l'annonce
			controllers.ProcessAnnouncement(tableID, seatIndex, userID, "square", height)
			return
		}
	}

	// Pas de carré
	hub.SendPrivateMessage(userID, map[string]interface{}{
		"type":    "GAME_EVENT",
		"message": "Carré invalide : vous n'avez pas 4 cartes de même hauteur",
		"isError": "true",
	})
}
