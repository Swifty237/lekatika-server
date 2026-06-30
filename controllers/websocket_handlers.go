package controllers

import (
	"encoding/json"
	"lekatika-server/database"
	"lekatika-server/models"
	"math/rand"
	"time"
)

// HandleSit génère les cartes pour un siège lorsqu'un joueur s'assoit
func HandleSit(tableID string, seatIndex int, userID uint) {
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
	// Si aucune carte n'est déjà générée, en générer 5
	if len(table.SeatCards[seatIndex].Hand) == 0 {
		hand := generateRandomHand()
		table.SeatCards[seatIndex].Hand = hand
		table.SeatCards[seatIndex].Played = []string{}
	}
	// Sauvegarder la table mise à jour
	updatedJSON, _ := json.Marshal(table)
	database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
	// Notifier tous les clients
	database.PublishTableUpdate(tableID)
}

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

// generateRandomHand génère 5 cartes aléatoires uniques
func generateRandomHand() []string {
	allCards := []string{
		"c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10",
		"d3", "d4", "d5", "d6", "d7", "d8", "d9", "d10",
		"h3", "h4", "h5", "h6", "h7", "h8", "h9", "h10",
		"s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10",
	}
	rand.Seed(time.Now().UnixNano())
	// Mélanger et prendre les 5 premières
	shuffled := make([]string, len(allCards))
	perm := rand.Perm(len(allCards))
	for i, v := range perm {
		shuffled[i] = allCards[v]
	}
	return shuffled[:5]
}
