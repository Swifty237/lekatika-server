package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"log"
	"time"
)

// Fonctions auxiliaires pour les cartes
func cardValue(card string) int {
	// Ex: "c3" -> 3, "s10" -> 10
	valStr := card[1:]
	if valStr == "10" {
		return 10
	}
	return int(valStr[0] - '0')
}

func cardSuit(card string) string {
	return string(card[0])
}

// getNextSeatIndex retourne le prochain siège occupé dans l'ordre croissant (1,2,3,4)
func getNextSeatIndex(table *models.PlayingTable, current int, checkCards bool) int {
	seats := table.Seats
	n := len(seats)
	for i := 1; i <= n; i++ {
		idx := (current + i) % n
		if seats[idx].UserID != 0 {
			if !checkCards || len(table.SeatCards[idx].Hand) > 0 {
				return idx
			}
		}
	}
	return -1
}

// getNextSeatIndexInTurn retourne le prochain siège occupé avec des cartes qui n'a pas encore joué dans ce tour.
func getNextSeatIndexInTurn(table *models.PlayingTable, current int) int {
	seats := table.Seats
	n := len(seats)
	for i := 1; i <= n; i++ {
		idx := (current + i) % n
		if seats[idx].UserID != 0 && len(table.SeatCards[idx].Hand) > 0 {
			// Vérifier si ce siège a déjà joué dans ce tour
			alreadyPlayed := false
			for _, played := range table.RoundPlayedCards {
				if played.SeatIndex == idx {
					alreadyPlayed = true
					break
				}
			}
			if !alreadyPlayed {
				return idx
			}
		}
	}
	return -1
}

// getFirstPlayer retourne le premier joueur à jouer (celui à gauche du dealer)
func getFirstPlayer(table *models.PlayingTable) int {
	dealerIdx := table.DealerSeatIndex
	if dealerIdx == -1 {
		return -1
	}
	return getNextSeatIndex(table, dealerIdx, true) // avec vérification des cartes
}

// StartHand démarre une nouvelle manche
func StartHand(table *models.PlayingTable) error {
	if table.GameStarted && table.CurrentRound == 0 {
		// Vérifier qu'il y a au moins 2 joueurs assis
		occupied := 0
		for _, seat := range table.Seats {
			if seat.UserID != 0 {
				occupied++
			}
		}
		if occupied < 2 {
			return fmt.Errorf("not enough players")
		}

		// Initialiser les variables de la manche
		table.CurrentRound = 1
		table.SuitRequired = ""
		table.RoundPlayedCards = []models.RoundCard{}
		table.RoundWinnerSeatIndex = -1
		table.LastRoundWinnerSeat = -1
		table.HandWinnerSeat = -1

		// Déterminer le premier joueur (pour plus tard)
		firstPlayer := getFirstPlayer(table)
		if firstPlayer == -1 {
			return fmt.Errorf("no first player found")
		}
		table.CurrentTurnSeatIndex = firstPlayer

		// Lancer la séquence de prélèvement des mises dans une goroutine
		go func(tableID string, tableModel models.PlayingTable) {
			// 1. Prélèvement des mises et mise à jour des sièges
			totalPot := 0
			for i, seat := range tableModel.Seats {
				if seat.UserID == 0 {
					continue
				}

				// userID := uint(seat.UserID)
				// Récupérer l'utilisateur depuis Redis pour vérifier le solde (optionnel)
				// ...

				// Déduire le montant du siège (AmountAtStake)
				newAmountAtStake := seat.AmountAtStake - tableModel.Bet
				if newAmountAtStake < 0 {
					newAmountAtStake = 0
				}
				tableModel.Seats[i].AmountAtStake = newAmountAtStake

				// Envoyer un événement SEAT_BET_UPDATE à tous les clients
				database.PublishSeatBetUpdate(tableID, i, newAmountAtStake, tableModel.Bet)

				totalPot += tableModel.Bet
			}

			//  Sauvegarder immédiatement la table avec les AmountAtStake modifiés
			tableJSON, _ := json.Marshal(tableModel)
			database.RedisClient.Set(database.Ctx, "table:"+tableID, tableJSON, 24*time.Hour)

			// 2. Attendre 3 secondes
			time.Sleep(3 * time.Second)

			// 3. Mettre à jour le pot et envoyer l'événement POT_UPDATE
			// Récupérer la table à jour (qui contient déjà les bons AmountAtStake)
			val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
			if err != nil {
				log.Printf("Erreur récupération table après délai: %v", err)
				return
			}
			var updatedTable models.PlayingTable
			if err := json.Unmarshal([]byte(val), &updatedTable); err != nil {
				log.Printf("Erreur désérialisation table après délai: %v", err)
				return
			}
			updatedTable.Pot = totalPot
			// Sauvegarder la table avec le pot
			SaveAndNotify(&updatedTable)

			// Envoyer l'événement POT_UPDATE
			database.PublishPotUpdate(tableID, totalPot)

			// (Les cartes sont déjà distribuées, le jeu continue)
		}(table.ID, *table)

		return nil
	}
	log.Printf("StartHand failed: GameStarted=%v, CurrentRound=%d", table.GameStarted, table.CurrentRound)
	return fmt.Errorf("game not started or already in hand")
}

func ProcessPlayCard(table *models.PlayingTable, seatIndex int, card string) error {
	// Vérifier que c'est le tour du joueur
	if table.CurrentTurnSeatIndex != seatIndex {
		return fmt.Errorf("not your turn")
	}
	// Vérifier que la carte est dans la main
	hand := &table.SeatCards[seatIndex].Hand
	found := false
	for i, c := range *hand {
		if c == card {
			// Retirer la carte de la main
			*hand = append((*hand)[:i], (*hand)[i+1:]...)
			table.SeatCards[seatIndex].Played = append(table.SeatCards[seatIndex].Played, card)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("card not in hand")
	}

	// Gestion de la couleur
	if table.SuitRequired != "" {
		if cardSuit(card) != table.SuitRequired {
			hasSuit := false
			for _, c := range *hand {
				if cardSuit(c) == table.SuitRequired {
					hasSuit = true
					break
				}
			}
			if hasSuit {
				// Remettre la carte dans la main
				table.SeatCards[seatIndex].Hand = append(table.SeatCards[seatIndex].Hand, card)
				return fmt.Errorf("you must play a card of suit %s", table.SuitRequired)
			}
		}
	} else {
		// Premier joueur du tour : choisit la couleur
		table.SuitRequired = cardSuit(card)
	}

	// Ajouter la carte aux cartes jouées du tour
	table.RoundPlayedCards = append(table.RoundPlayedCards, models.RoundCard{
		SeatIndex: seatIndex,
		Card:      card,
	})

	// Passer au joueur suivant (qui n'a pas encore joué dans ce tour)
	nextPlayer := getNextSeatIndexInTurn(table, seatIndex)
	table.CurrentTurnSeatIndex = nextPlayer

	if nextPlayer == -1 {
		// Tous les joueurs ayant des cartes ont joué dans ce tour
		processRoundEnd(table)
	} else {
		SaveAndNotify(table)
	}

	return nil
}

// processRoundEnd gère la fin d'un tour
func processRoundEnd(table *models.PlayingTable) {
	// Trouver la meilleure carte de la couleur requise
	var bestCard string
	bestSeat := -1
	for _, played := range table.RoundPlayedCards {
		if cardSuit(played.Card) == table.SuitRequired {
			if bestSeat == -1 || cardValue(played.Card) > cardValue(bestCard) {
				bestCard = played.Card
				bestSeat = played.SeatIndex
			}
		}
	}

	if bestSeat == -1 {
		// Fallback : prendre le premier siège avec des cartes jouées
		if len(table.RoundPlayedCards) > 0 {
			bestSeat = table.RoundPlayedCards[0].SeatIndex
		} else {
			// Très improbable, mais on prend le premier siège occupé
			for i, seat := range table.Seats {
				if seat.UserID != 0 {
					bestSeat = i
					break
				}
			}
		}
	}

	table.RoundWinnerSeatIndex = bestSeat
	table.LastRoundWinnerSeat = bestSeat

	if table.CurrentRound == 5 {
		table.HandWinnerSeat = bestSeat
		table.DealerSeatIndex = bestSeat
		AwardPotToWinner(table, bestSeat)

		// Sauvegarder la table avec les mains intactes (les joueurs voient encore leurs cartes)
		SaveAndNotify(table)

		// Lancer la séquence d'événements et de redistribution
		go func(tableID string, winnerSeat int) {
			// 1. "Fin de manche" → immédiat
			database.PublishGameEvent(tableID, "Fin de manche")

			time.Sleep(3 * time.Second)

			// 2. "X gagne la manche" → après 4s
			username := GetUsernameFromSeat(tableID, winnerSeat)
			database.PublishGameEvent(tableID, username+" gagne la manche")

			time.Sleep(3 * time.Second)

			// 3. "Début de la nouvelle manche dans quelques secondes" → après 8s
			database.PublishGameEvent(tableID, "Début de la nouvelle manche")

			time.Sleep(3 * time.Second)

			// 4. Récupérer la table à jour, vider les mains et réinitialiser → après 6s
			val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
			if err != nil {
				log.Printf("Erreur récupération table pour vidage: %v", err)
				return
			}
			var updatedTable models.PlayingTable
			if err := json.Unmarshal([]byte(val), &updatedTable); err != nil {
				log.Printf("Erreur désérialisation pour vidage: %v", err)
				return
			}

			// Réinitialiser les variables de la manche
			updatedTable.CurrentRound = 0
			updatedTable.SuitRequired = ""
			updatedTable.RoundPlayedCards = []models.RoundCard{}
			updatedTable.CurrentTurnSeatIndex = -1

			// Vider les mains et les cartes jouées
			for i := range updatedTable.SeatCards {
				updatedTable.SeatCards[i].Hand = []string{}
				updatedTable.SeatCards[i].Played = []string{}
			}

			// Sauvegarder la table vidée
			SaveAndNotify(&updatedTable)

			// Lancer la distribution pour la nouvelle manche
			DistributeCardsForHand(tableID)
		}(table.ID, bestSeat)

	} else {
		// Tour suivant
		table.CurrentRound++
		table.CurrentTurnSeatIndex = table.RoundWinnerSeatIndex
		if len(table.SeatCards[table.CurrentTurnSeatIndex].Hand) == 0 {
			table.CurrentTurnSeatIndex = getNextSeatIndexInTurn(table, table.CurrentTurnSeatIndex)
		}
		table.SuitRequired = ""
		table.RoundPlayedCards = []models.RoundCard{}
		SaveAndNotify(table)
	}
}

// awardPotToWinner ajoute le pot à la bankroll du gagnant
func AwardPotToWinner(table *models.PlayingTable, seatIndex int) {
	if table.Pot <= 0 {
		return
	}
	// Ajouter le pot au AmountAtStake du siège gagnant
	if seatIndex >= 0 && seatIndex < len(table.Seats) {
		table.Seats[seatIndex].AmountAtStake += table.Pot
	}
	// Remettre le pot à 0
	table.Pot = 0
}

// saveAndNotify sauvegarde la table et notifie les clients
func SaveAndNotify(table *models.PlayingTable) {
	key := "table:" + table.ID
	updatedJSON, _ := json.Marshal(table)
	database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
	database.PublishTableUpdate(table.ID)
}

// Démarre une nouvelle distribution pour une nouvelle manche
func startNewHand(tableID string) {
	// Récupérer la table actuelle
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return
	}
	// Si la table n'a pas au moins 2 joueurs, ne pas distribuer
	occupied := 0
	for _, seat := range table.Seats {
		if seat.UserID != 0 {
			occupied++
		}
	}
	if occupied < 2 {
		return
	}
	// On peut lancer la distribution progressive (comme dans SitAtTable)
	// en utilisant une goroutine
	go DistributeCardsForHand(tableID) // à définir
}

// distributeCardsForHand distribue les cartes pour une nouvelle manche (5 cartes à chaque joueur assis)
// distributeCardsForHand distribue les cartes pour une nouvelle manche (5 cartes à chaque joueur assis)
func DistributeCardsForHand(tableID string) {
	// Récupérer la table
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		log.Printf("distributeCardsForHand: impossible de récupérer la table %s", tableID)
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		log.Printf("distributeCardsForHand: erreur de désérialisation")
		return
	}

	// Vider les mains de tous les joueurs au début
	for i := range table.SeatCards {
		table.SeatCards[i].Hand = []string{}
		table.SeatCards[i].Played = []string{}
	}

	// Vérifier qu'il y a au moins 2 joueurs assis
	occupied := 0
	for _, seat := range table.Seats {
		if seat.UserID != 0 {
			occupied++
		}
	}
	if occupied < 2 {
		log.Printf("distributeCardsForHand: pas assez de joueurs (%d)", occupied)
		return
	}

	// Déterminer le dealer
	dealerIdx := table.DealerSeatIndex
	if dealerIdx == -1 {
		for i, seat := range table.Seats {
			if seat.UserID != 0 {
				dealerIdx = i
				table.DealerSeatIndex = i
				break
			}
		}
	}

	// --- CORRECTION : Construire l'ordre circulaire à partir du dealer ---
	// Récupérer tous les sièges occupés
	occupiedSeats := []int{}
	for i, seat := range table.Seats {
		if seat.UserID != 0 {
			occupiedSeats = append(occupiedSeats, i)
		}
	}

	// Trier les sièges dans l'ordre circulaire en partant de dealer+1
	seatsIndices := []int{}
	n := len(table.Seats)
	// On commence par le siège après le dealer
	start := (dealerIdx + 1) % n
	for i := 0; i < len(occupiedSeats); i++ {
		idx := (start + i) % n
		// Vérifier si ce siège est occupé
		for _, occ := range occupiedSeats {
			if occ == idx {
				seatsIndices = append(seatsIndices, idx)
				break
			}
		}
	}
	// Si le dealer n'est pas le dernier, on l'ajoute s'il est occupé (il doit l'être)
	if dealerIdx >= 0 && table.Seats[dealerIdx].UserID != 0 {
		// Il est déjà dans la liste? On le met à la fin
		// On le retire s'il a été ajouté (normalement non, car on a commencé après lui)
		// Pour être sûr, on le retire et on l'ajoute à la fin
		newSeats := []int{}
		for _, s := range seatsIndices {
			if s != dealerIdx {
				newSeats = append(newSeats, s)
			}
		}
		seatsIndices = append(newSeats, dealerIdx)
	}

	log.Printf("DistributeCards: dealerIdx=%d, seatsIndices=%v", dealerIdx, seatsIndices)
	// --- Fin correction ---

	// Créer un nouveau deck
	table.Deck = createDeck()

	// Fonction pour distribuer des cartes
	dealCards := func(count int) ([]string, error) {
		if len(table.Deck) < count {
			return nil, fmt.Errorf("pas assez de cartes (reste %d)", len(table.Deck))
		}
		cards := []string{}
		for i := 0; i < count; i++ {
			cards = append(cards, table.Deck[0])
			table.Deck = table.Deck[1:]
		}
		return cards, nil
	}

	// Distribution en deux tours : 3 puis 2 cartes
	for round := 0; round < 2; round++ {
		cardsCount := 3
		if round == 1 {
			cardsCount = 2
		}
		for _, seatIdx := range seatsIndices {
			cards, err := dealCards(cardsCount)
			if err != nil {
				log.Printf("Erreur distribution pour siège %d: %v", seatIdx, err)
				return
			}
			table.SeatCards[seatIdx].Hand = append(table.SeatCards[seatIdx].Hand, cards...)

			updatedJSON, _ := json.Marshal(table)
			database.RedisClient.Set(database.Ctx, "table:"+tableID, updatedJSON, 24*time.Hour)
			dealEvent := map[string]interface{}{
				"type":      "DEAL",
				"tableId":   tableID,
				"seatIndex": seatIdx,
				"cards":     cards,
			}
			dealJSON, _ := json.Marshal(dealEvent)
			database.RedisClient.Publish(database.Ctx, "tables", dealJSON)
			time.Sleep(1 * time.Second)
		}
	}

	// Démarrer la manche
	table.GameStarted = true
	if err := StartHand(&table); err != nil {
		log.Printf("Erreur StartHand: %v", err)
	}
	updatedJSON, _ := json.Marshal(table)
	database.RedisClient.Set(database.Ctx, "table:"+tableID, updatedJSON, 24*time.Hour)
	database.PublishTableUpdate(tableID)
}

func GetUsernameFromSeat(tableID string, seatIndex int) string {
	key := "table:" + tableID
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return "Joueur"
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return "Joueur"
	}
	if seatIndex < 0 || seatIndex >= len(table.Seats) {
		return "Joueur"
	}
	userID := uint(table.Seats[seatIndex].UserID)
	userKey := fmt.Sprintf("user:%d", userID)
	userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
	if err != nil {
		return fmt.Sprintf("Joueur %d", userID)
	}
	var user models.UserRedis
	if err := json.Unmarshal([]byte(userVal), &user); err != nil {
		return fmt.Sprintf("Joueur %d", userID)
	}
	return user.Username
}

// sendGameError envoie un message d'erreur via WebSocket
// func sendGameError(tableID string, message string) {
// 	event := map[string]string{
// 		"type":    "GAME_EVENT",
// 		"tableId": tableID,
// 		"message": message,
// 		"isError": "true",
// 	}
// 	eventJSON, _ := json.Marshal(event)
// 	database.RedisClient.Publish(database.Ctx, "tables", eventJSON)
// }
