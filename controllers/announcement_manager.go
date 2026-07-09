// controllers/announcement_manager.go
package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"log"
	"strings"
	"sync"
	"time"
)

var (
	timersMu sync.Mutex
	timers   = make(map[string]*time.Timer)
)

// Announcement représente une annonce de jeu gagnant
type Announcement struct {
	SeatIndex int    `json:"seatIndex"`
	UserID    uint   `json:"userID"` // Ajout
	Type      string `json:"type"`
	Value     int    `json:"value"`
	Timestamp int64  `json:"timestamp"`
}

// getAllCards retourne la concaténation de la main et des cartes jouées
func getAllCards(seatCards models.SeatCards) []string {
	all := append([]string{}, seatCards.Hand...)
	all = append(all, seatCards.Played...)
	return all
}

// hasSquare vérifie si un carré est présent et retourne la valeur de la carte (ex: 9 pour un carré de 9)
func hasSquare(cards []string) (int, bool) {
	// Compter les occurrences de chaque valeur (1 à 13, mais on utilise les valeurs 2-10, Valet=11, Dame=12, Roi=13, As=14 ?
	// Dans votre code, CardValue retourne la valeur numérique de la carte (ex: c9 -> 9, s10 -> 10).
	// On suppose que les valeurs sont 2..10, et les figures ont des valeurs spécifiques selon vos règles.
	// Pour l'instant, on se contente de compter les valeurs.
	count := make(map[int]int)
	for _, card := range cards {
		v := CardValue(card)
		count[v]++
	}
	for val, cnt := range count {
		if cnt >= 4 {
			return val, true
		}
	}
	return 0, false
}

// hasThreeSeven vérifie la présence d'au moins 3 sept et retourne la somme totale des valeurs des 5 cartes
func hasThreeSeven(cards []string) (int, bool) {
	count7 := 0
	sum := 0
	for _, card := range cards {
		v := CardValue(card)
		sum += v
		if v == 7 {
			count7++
		}
	}
	if count7 >= 3 {
		return sum, true
	}
	return 0, false
}

// hasTia vérifie que le joueur a exactement 5 cartes (main + jouées)
// et que la somme de leurs valeurs est <= 21.
// Retourne la somme totale et true si valide.
func hasTia(cards []string) (int, bool) {
	if len(cards) != 5 {
		return 0, false
	}
	sum := 0
	for _, card := range cards {
		sum += CardValue(card)
	}
	if sum <= 21 {
		return sum, true
	}
	return 0, false
}

// CompareAnnouncements compare deux annonces selon la hiérarchie.
// Retourne true si a est plus forte que b.
func CompareAnnouncements(a, b Announcement) bool {
	typeRank := map[string]int{"square": 3, "three_seven": 2, "tia": 1}
	rankA := typeRank[a.Type]
	rankB := typeRank[b.Type]
	log.Printf("[COMPARE] a: type=%s, value=%d, rank=%d | b: type=%s, value=%d, rank=%d", a.Type, a.Value, rankA, b.Type, b.Value, rankB)
	if rankA != rankB {
		result := rankA > rankB
		log.Printf("[COMPARE] ranks differ, result=%v", result)
		return result
	}
	// mêmes types
	switch a.Type {
	case "square":
		result := a.Value > b.Value
		log.Printf("[COMPARE] square: result=%v", result)
		return result
	case "tia":
		result := a.Value < b.Value
		log.Printf("[COMPARE] tia: result=%v", result)
		return result
	case "three_seven":
		result := a.Value > b.Value
		log.Printf("[COMPARE] three_seven: result=%v", result)
		return result
	}
	log.Printf("[COMPARE] fallback: false")
	return false
}

// ProcessAnnouncement est la fonction publique à appeler depuis les gestionnaires
func ProcessAnnouncement(tableID string, seatIndex int, userID uint, annType string, value int) {
	err := AddAnnouncement(tableID, seatIndex, userID, annType, value)
	if err != nil {
		log.Printf("Erreur lors de l'ajout de l'annonce: %v", err)
		// Ici vous pouvez envoyer un message privé via le hub si vous voulez
	}
}

// AddAnnouncement ajoute une annonce pour une table
func AddAnnouncement(tableID string, seatIndex int, userID uint, annType string, value int) error {
	// Normalisation
	annType = strings.TrimSpace(strings.ToLower(annType))
	validTypes := map[string]bool{"square": true, "three_seven": true, "tia": true}
	if !validTypes[annType] {
		return fmt.Errorf("type invalide: %s", annType)
	}

	// Récupérer la table
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		return fmt.Errorf("table introuvable: %v", err)
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return fmt.Errorf("erreur désérialisation: %v", err)
	}

	// Vérifier le siège
	if seatIndex < 0 || seatIndex >= len(table.Seats) || uint(table.Seats[seatIndex].UserID) != userID {
		return fmt.Errorf("siège invalide ou userID ne correspond pas")
	}

	playedCount := len(table.SeatCards[seatIndex].Played)
	if playedCount >= 2 {
		log.Printf("[REJET] table=%s, user=%d, type=%s, cartes jouées=%d (>=2) – annulation", tableID, userID, annType, playedCount)
		return fmt.Errorf("Vous avez jouer au moins 2 cartes, vous ne pouvez plus annoncer")
	}

	// Récupérer toutes les cartes du joueur (main + jouées)
	allCards := getAllCards(table.SeatCards[seatIndex])

	// Valider la combinaison et calculer la valeur réelle
	var actualValue int
	var valid bool
	switch annType {
	case "square":
		actualValue, valid = hasSquare(allCards)
	case "three_seven":
		actualValue, valid = hasThreeSeven(allCards)
	case "tia":
		actualValue, valid = hasTia(allCards)
	default:
		return fmt.Errorf("type inconnu")
	}

	if annType == "tia" {
		log.Printf("[TIA] table=%s, user=%d, cartes=%v, somme=%d", tableID, userID, allCards, actualValue)
	}

	if !valid {
		log.Printf("[REJET] table=%s, user=%d, type=%s, main=%v", tableID, userID, annType, allCards)
		// Convertir le type en français
		var typeName string
		switch annType {
		case "square":
			typeName = "carré"
		case "three_seven":
			typeName = "3 sept"
		case "tia":
			typeName = "tia"
		default:
			typeName = annType
		}
		return fmt.Errorf("Vous n'avez pas %s", typeName)
	}

	// Si la valeur envoyée diffère de la valeur réelle, on la corrige et on log
	if actualValue != value {
		log.Printf("[CORRECTION] table=%s, user=%d, type=%s, envoyée=%d, réelle=%d", tableID, userID, annType, value, actualValue)
		value = actualValue
	}

	// Révéler les cartes du joueur qui annonce
	if table.RevealedSeats == nil {
		table.RevealedSeats = make([]bool, len(table.Seats))
	}

	table.RevealedSeats[seatIndex] = true

	// Sauvegarder la table avec les cartes révélées et notifier tous les clients
	SaveAndNotify(&table)

	// Vérifier que le joueur n'a pas déjà annoncé
	playerKey := "table:" + tableID + ":announced_players"
	playerField := fmt.Sprintf("%d", userID)
	exists, err := database.RedisClient.SIsMember(database.Ctx, playerKey, playerField).Result()
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("player already announced")
	}

	// Créer l'annonce avec la valeur corrigée
	ann := Announcement{
		SeatIndex: seatIndex,
		UserID:    userID,
		Type:      annType,
		Value:     value,
		Timestamp: time.Now().UnixNano(),
	}

	data, _ := json.Marshal(ann)
	key := "table:" + tableID + ":announcements"
	if err := database.RedisClient.RPush(database.Ctx, key, data).Err(); err != nil {
		return err
	}
	if err := database.RedisClient.SAdd(database.Ctx, playerKey, playerField).Err(); err != nil {
		return err
	}

	scheduleResolution(tableID)
	log.Printf("[ANNONCE ACCEPTÉE] table=%s, user=%d, type=%s, value=%d", tableID, userID, annType, value)

	// Récupérer le nom d'utilisateur
	username := GetUsernameByUserID(userID)
	if username == "" {
		username = fmt.Sprintf("Joueur %d", userID)
	}

	// Convertir le type en français (déjà utilisé dans la partie rejet)
	var typeName string
	switch annType {
	case "square":
		typeName = "carré"
	case "three_seven":
		typeName = "3 sept"
	case "tia":
		typeName = "tia"
	default:
		typeName = annType
	}

	// Envoyer le message à tous les clients de la table
	database.PublishGameEvent(tableID, fmt.Sprintf("%s annonce %s", username, typeName))

	return nil
}

// scheduleResolution planifie la résolution des annonces
func scheduleResolution(tableID string) {
	timersMu.Lock()
	defer timersMu.Unlock()

	if timer, ok := timers[tableID]; ok {
		timer.Stop()
		delete(timers, tableID)
	}

	timers[tableID] = time.AfterFunc(5*time.Second, func() {
		finalizeWinner(tableID)
	})
}

// finalizeWinner sélectionne le gagnant et applique la victoire
func finalizeWinner(tableID string) {
	timersMu.Lock()
	delete(timers, tableID)
	timersMu.Unlock()

	anns, err := getAnnouncements(tableID)
	if err != nil || len(anns) == 0 {
		return
	}

	best := getBestAnnouncement(anns)
	if best == nil {
		return
	}

	log.Printf("[RÉSOLUTION] table=%s, nb annonces=%d", tableID, len(anns))
	applyWinner(tableID, *best)
	clearAnnouncements(tableID)
}

// getBestAnnouncement retourne la meilleure annonce
func getBestAnnouncement(anns []Announcement) *Announcement {
	if len(anns) == 0 {
		return nil
	}
	best := &anns[0]
	for i := 1; i < len(anns); i++ {
		if CompareAnnouncements(anns[i], *best) {
			log.Printf("[BEST-UPDATE] ancien: user=%d, type=%s, value=%d -> nouveau: user=%d, type=%s, value=%d",
				best.UserID, best.Type, best.Value, anns[i].UserID, anns[i].Type, anns[i].Value)
			best = &anns[i]
		} else {
			log.Printf("[BEST-KEEP] conservé: user=%d, type=%s, value=%d contre user=%d, type=%s, value=%d",
				best.UserID, best.Type, best.Value, anns[i].UserID, anns[i].Type, anns[i].Value)
		}
	}
	log.Printf("[BEST] user=%d, type=%s, value=%d", best.UserID, best.Type, best.Value)
	return best
}

// clearAnnouncements supprime toutes les annonces
func clearAnnouncements(tableID string) {
	database.RedisClient.Del(database.Ctx, "table:"+tableID+":announcements")
	database.RedisClient.Del(database.Ctx, "table:"+tableID+":announced_players")
}

// getAnnouncements récupère toutes les annonces
func getAnnouncements(tableID string) ([]Announcement, error) {
	key := "table:" + tableID + ":announcements"
	vals, err := database.RedisClient.LRange(database.Ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	anns := []Announcement{}
	for _, v := range vals {
		var a Announcement
		if err := json.Unmarshal([]byte(v), &a); err == nil {
			anns = append(anns, a)
		}
	}
	log.Printf("[ANNONCES BRUTES] table=%s, vals=%v", tableID, vals)
	return anns, nil
}

// applyWinner applique la victoire pour une annonce
func applyWinner(tableID string, ann Announcement) {
	val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
	if err != nil {
		return
	}
	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		return
	}

	seatIndex := ann.SeatIndex
	username := GetUsernameByUserID(ann.UserID)
	log.Printf("[WINNER] userID=%d, username=%s, type=%s, value=%d", ann.UserID, username, ann.Type, ann.Value)

	// table.ThreeSevenSeat = seatIndex
	table.DealerSeatIndex = seatIndex

	AwardPotToWinner(&table, seatIndex)

	SaveAndNotify(&table)

	var msg string
	switch ann.Type {
	case "square":
		msg = fmt.Sprintf("%s gagne la manche avec un carré de %d !", username, ann.Value)
	case "three_seven":
		msg = username + " gagne la manche avec 3 sept !"
	case "tia":
		msg = username + " gagne la manche avec Tia !"
	}
	log.Printf("[APPLICATION] table=%s, seat=%d, userID=%d, type=%s, value=%d", tableID, ann.SeatIndex, ann.UserID, ann.Type, ann.Value)
	log.Printf("[WINNER] user=%d, type=%s, value=%d", ann.UserID, ann.Type, ann.Value)
	database.PublishGameEvent(tableID, msg)

	go func() {
		time.Sleep(3 * time.Second)
		val, err := database.RedisClient.Get(database.Ctx, "table:"+tableID).Result()
		if err != nil {
			return
		}
		var updatedTable models.PlayingTable
		if err := json.Unmarshal([]byte(val), &updatedTable); err != nil {
			return
		}
		updatedTable.CurrentRound = 0
		updatedTable.SuitRequired = ""
		updatedTable.RoundPlayedCards = []models.RoundCard{}
		updatedTable.CurrentTurnSeatIndex = -1
		updatedTable.ThreeSevenSeat = -1
		for i := range updatedTable.SeatCards {
			updatedTable.SeatCards[i].Hand = []string{}
			updatedTable.SeatCards[i].Played = []string{}
		}
		SaveAndNotify(&updatedTable)
		DistributeCardsForHand(tableID)
	}()
}
