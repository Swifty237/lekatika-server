package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"log"
	"net/http"
	"strconv"
	"time"

	"math/rand"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SitInput struct {
	Amount int `json:"amount"`
}

type CreateTableInput struct {
	Name        string `json:"name"` // Nouveau
	IsPrivate   bool   `json:"is_private"`
	IsRealMoney bool   `json:"is_real_money"`
	Paid33      bool   `json:"paid_33"`
	Bet         int    `json:"bet"`
}

func CreateTable(c *gin.Context) {
	var input CreateTableInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Récupérer l'ID de l'utilisateur connecté (via JWT)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	tableID := uuid.New().String()
	inviteLink := fmt.Sprintf("/join/%s", tableID)

	seat1 := models.Seat{}
	seat2 := models.Seat{}
	seat3 := models.Seat{}
	seat4 := models.Seat{}

	table := models.PlayingTable{
		ID:              tableID,
		Name:            input.Name, // ← utilisation
		CreatedBy:       userID.(uint),
		IsPrivate:       input.IsPrivate,
		IsRealMoney:     input.IsRealMoney,
		Paid33:          input.Paid33,
		Bet:             input.Bet,
		Status:          "waiting",
		Players:         []uint{userID.(uint)},
		PlayerUsernames: []string{},
		CreatedAt:       time.Now(),
		Seats:           []models.Seat{seat1, seat2, seat3, seat4},
		SeatsConnected:  []bool{false, false, false, false}, // initialement tous déconnectés (siège vide)
		SeatCards: []models.SeatCards{
			{Hand: []string{}, Played: []string{}},
			{Hand: []string{}, Played: []string{}},
			{Hand: []string{}, Played: []string{}},
			{Hand: []string{}, Played: []string{}},
		},
		DealerSeatIndex:       -1,
		Turn:                  "",
		LastWinningSeat:       "",
		LastRoundWinner:       "",
		ThreeSevenSeat:        -1,
		Pot:                   0,
		HandOver:              false,
		HandCompleted:         false,
		WinMessages:           []string{},
		GameNotifications:     []string{},
		History:               []string{},
		SeatTurnTimer:         []string{},
		DemandedSuit:          []string{},
		CurrentRoundCards:     []string{},
		RoundNumber:           0,
		CountHand:             0,
		HandParticipants:      []string{},
		WonByCombination:      false,
		OnTurnChanged:         []string{},
		ChatRoom:              []string{},
		ChatMessages:          []models.ChatMessage{},
		InviteLink:            inviteLink,
		DisconnectedAt:        []int64{0, 0, 0, 0},
		Deck:                  []string{},
		GameStarted:           false,
		Starting:              false,
		Dealing:               false,
		CurrentRound:          0,
		CurrentTurnSeatIndex:  -1,
		SuitRequired:          "",
		RoundPlayedCards:      []models.RoundCard{},
		RoundWinnerSeatIndex:  -1,
		LastRoundWinnerSeat:   -1,
		HandWinnerSeat:        -1,
		RevealedSeats:         []bool{false, false, false, false},
		ParticipatingSeats:    []bool{false, false, false, false},
		PausedSeats:           []bool{false, false, false, false},
		IsDealing:             false,
		DistributionCancelled: false,
		RoundHistory:          []models.RoundHistoryEntry{},
	}

	// Stocker dans Redis (clé: table:{ID})
	tableJSON, err := json.Marshal(table)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize table"})
		return
	}

	err = database.RedisClient.Set(database.Ctx, "table:"+tableID, tableJSON, 24*time.Hour).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save table"})
		return
	}

	// Mettre à jour l'utilisateur dans Redis : ajouter cette table à sa liste
	AddTableToUser(userID.(uint), tableID)

	usernames, _ := fetchUsernames(table.Players)
	table.PlayerUsernames = usernames

	// Notifier tous les clients (via WebSocket) que la liste des tables a changé
	database.PublishTablesReload()

	c.JSON(http.StatusOK, gin.H{
		"message": "Table created successfully",
		"table":   table,
	})
}

func GetTable(c *gin.Context) {
	tableID := c.Param("id")
	key := "table:" + tableID

	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		return
	}

	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse table data"})
		return
	}

	usernames, _ := fetchUsernames(table.Players)
	table.PlayerUsernames = usernames

	c.JSON(http.StatusOK, gin.H{"table": table})
}

func JoinTable(c *gin.Context) {
	tableID := c.Param("id")
	userID, _ := c.Get("user_id")

	// 1. Récupérer la table depuis Redis
	key := "table:" + tableID
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		return
	}

	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse table data"})
		return
	}

	// 2. Vérifier si la table est déjà pleine (à adapter selon votre logique, par exemple 4 joueurs max)
	if len(table.Players) >= 4 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Table is full"})
		return
	}

	// 3. Ajouter le nouvel utilisateur s'il n'est pas déjà dans la partie
	alreadyIn := false
	for _, player := range table.Players {
		if player == userID.(uint) {
			alreadyIn = true
			break
		}
	}
	if !alreadyIn {
		table.Players = append(table.Players, userID.(uint))
	}

	usernames, _ := fetchUsernames(table.Players)
	table.PlayerUsernames = usernames

	// 4. Sauvegarder la table mise à jour dans Redis
	updatedTableJSON, err := json.Marshal(table)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize table"})
		return
	}

	err = database.RedisClient.Set(database.Ctx, key, updatedTableJSON, 24*time.Hour).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save table"})
		return
	}

	// Mettre à jour l'utilisateur dans Redis : ajouter cette table à sa liste
	AddTableToUser(userID.(uint), tableID)

	c.JSON(http.StatusOK, gin.H{"message": "Joined table successfully", "table": table})
}

func ListTables(c *gin.Context) {
	// Récupérer toutes les clés "table:*"
	keys, err := database.RedisClient.Keys(database.Ctx, "table:*").Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list tables"})
		return
	}

	tables := []models.PlayingTable{}
	for _, key := range keys {
		val, err := database.RedisClient.Get(database.Ctx, key).Result()
		if err != nil {
			continue
		}
		var table models.PlayingTable
		if err := json.Unmarshal([]byte(val), &table); err == nil {
			tables = append(tables, table)
		}
	}

	c.JSON(http.StatusOK, gin.H{"tables": tables})
}

func LeaveTable(c *gin.Context) {
	tableID := c.Param("id")
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	key := "table:" + tableID
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		return
	}

	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse table data"})
		return
	}

	// 1. Restituer le montant misé si le joueur est assis
	var seatAmount int
	seatIndex := -1
	for i, seat := range table.Seats {
		if seat.UserID == int(userID.(uint)) {
			seatAmount = seat.AmountAtStake
			seatIndex = i
			break
		}
	}
	if seatAmount > 0 {
		// Restituer à la bankroll (comme dans UnseatFromTable)
		userKey := fmt.Sprintf("user:%d", userID)
		userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
		if err == nil {
			var userRedis models.UserRedis
			if err := json.Unmarshal([]byte(userVal), &userRedis); err == nil {
				if userRedis.FreeChipsAmountBankroll != nil {
					newBankroll := *userRedis.FreeChipsAmountBankroll + float64(seatAmount)
					userRedis.FreeChipsAmountBankroll = &newBankroll
					updatedUserJSON, _ := json.Marshal(userRedis)
					database.RedisClient.Set(database.Ctx, userKey, updatedUserJSON, 72*time.Hour)
				}
			}
		}
		database.DB.Model(&models.User{}).Where("id = ?", userID).
			Update("free_chips_amount_bankroll", gorm.Expr("free_chips_amount_bankroll + ?", seatAmount))

	}
	if seatIndex != -1 {
		table.Seats[seatIndex].UserID = 0
		table.Seats[seatIndex].AmountAtStake = 0
		table.SeatsConnected[seatIndex] = false
	}

	// Sauvegarder la table
	SaveAndNotify(&table)

	// Après avoir retiré le joueur et sauvegardé
	if table.GameStarted && table.CurrentTurnSeatIndex == seatIndex {
		advanceTurnAfterLeave(&table)
	}

	// Si une distribution est en cours, on annule
	if table.IsDealing {
		table.DistributionCancelled = true
		SaveAndNotify(&table)
		// Ne pas appeler checkAndHandleHandEndOnLeave ici, ce sera fait par DistributeCardsForHand
	}

	// Vérifier la fin de manche
	checkAndHandleHandEndOnLeave(&table)

	if table.DealerSeatIndex == seatIndex {
		newDealer := -1
		for i, seat := range table.Seats {
			if seat.UserID != 0 && i != seatIndex {
				newDealer = i
				break
			}
		}
		table.DealerSeatIndex = newDealer
	}

	occupiedSeats := 0
	for _, seat := range table.Seats {
		if seat.UserID != 0 {
			occupiedSeats++
		}
	}
	if occupiedSeats < 2 {
		table.GameStarted = false
		table.Starting = false
		table.Deck = []string{}
		for i := range table.SeatCards {
			table.SeatCards[i].Hand = []string{}
			table.SeatCards[i].Played = []string{}
		}
	}

	// 2. Supprimer l'utilisateur de la liste des joueurs
	newPlayers := []uint{}
	for _, playerID := range table.Players {
		if playerID != userID.(uint) {
			newPlayers = append(newPlayers, playerID)
		}
	}
	table.Players = newPlayers

	usernames, _ := fetchUsernames(table.Players)
	table.PlayerUsernames = usernames

	// 3. Si plus aucun joueur, supprimer la table
	if len(table.Players) == 0 {
		database.RedisClient.Del(database.Ctx, key)
		RemoveTableFromUser(userID.(uint), tableID)
		database.PublishTablesReload()
		database.PublishTableUpdate(tableID)
		c.JSON(http.StatusOK, gin.H{"message": "Table deleted because no players left"})
		return
	}

	// 4. Sauvegarder la table mise à jour
	updatedJSON, err := json.Marshal(table)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize table"})
		return
	}
	err = database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save table"})
		return
	}

	// 5. Notifier les clients
	database.PublishTablesReload()
	database.PublishTableUpdate(tableID)
	c.JSON(http.StatusOK, gin.H{"message": "Left table successfully"})
}

func GetTablesForUser(userID uint) ([]string, error) {
	var tableIDs []string
	keys, err := database.RedisClient.Keys(database.Ctx, "table:*").Result()
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		val, err := database.RedisClient.Get(database.Ctx, key).Result()
		if err != nil {
			continue
		}
		var table models.PlayingTable
		if err := json.Unmarshal([]byte(val), &table); err != nil {
			continue
		}
		for _, player := range table.Players {
			if player == userID {
				tableIDs = append(tableIDs, table.ID)
				break
			}
		}
	}
	return tableIDs, nil
}

func SitAtTable(c *gin.Context) {
	tableID := c.Param("id")
	seatIDStr := c.Param("seatId")
	seatIndex, err := strconv.Atoi(seatIDStr)
	if err != nil || seatIndex < 1 || seatIndex > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seat number"})
		return
	}
	seatIndex-- // 0-based

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var input SitInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or invalid amount"})
		return
	}

	// ---------- 1. Récupérer la table depuis Redis ----------
	key := "table:" + tableID
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		return
	}

	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse table data"})
		return
	}

	// Vérifications de base : siège libre, utilisateur déjà assis ailleurs, etc.
	for i, seat := range table.Seats {
		if seat.UserID == int(userID.(uint)) {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Already seated at seat %d", i+1)})
			return
		}
	}

	if seatIndex >= len(table.Seats) || table.Seats[seatIndex].UserID != 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Seat already taken"})
		return
	}

	// Vérifier si une distribution est en cours
	if table.IsDealing {
		// Ajouter la demande dans Redis (liste) pour éviter les écrasements
		pendingKey := "table:" + tableID + ":pending_sits"
		req := models.PendingSitRequest{
			UserID:    userID.(uint),
			SeatIndex: seatIndex,
			Amount:    input.Amount,
			Timestamp: time.Now().UnixNano(),
		}
		reqJSON, _ := json.Marshal(req)
		err := database.RedisClient.RPush(database.Ctx, pendingKey, reqJSON).Err()
		if err != nil {
			log.Printf("Erreur lors de l'ajout de la demande en attente: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur interne"})
			return
		}

		// Répondre au client (sans modifier la table)
		c.JSON(http.StatusAccepted, gin.H{
			"message": "distribution en cours",
			"table":   table, // la table actuelle (sans le joueur)
		})
		return
	}

	// ---------- 2. Vérifier la bankroll de l'utilisateur ----------
	// On récupère l'utilisateur depuis Redis (cache)
	userKey := fmt.Sprintf("user:%d", userID)
	userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
	var userRedis models.UserRedis
	var bankroll *float64

	if err == nil {
		if err := json.Unmarshal([]byte(userVal), &userRedis); err == nil {
			bankroll = userRedis.FreeChipsAmountBankroll
		}
	} else {
		// Fallback : lire depuis PostgreSQL
		var user models.User
		if err := database.DB.First(&user, userID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		bankroll = user.FreeChipsAmountBankroll
		// Reconstruire UserRedis pour le mettre à jour plus tard
		userRedis = models.UserRedis{
			Model:                   gorm.Model{ID: user.ID},
			Username:                user.Username,
			Email:                   user.Email,
			FreeChipsAmountBankroll: user.FreeChipsAmountBankroll,
			RealChipsAmountBankroll: user.RealChipsAmountBankroll,
			ProfilePictureLink:      user.ProfilePictureLink,
			LastModification:        user.LastModification,
			PlayingTableIDs:         []string{}, // à charger si besoin
		}
	}

	// Vérifier que la bankroll est suffisante
	if bankroll == nil || *bankroll < float64(input.Amount) {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": "Insufficient chips"})
		return
	}

	// ---------- 3. Déduire le montant de la bankroll ----------
	newBankroll := *bankroll - float64(input.Amount)
	userRedis.FreeChipsAmountBankroll = &newBankroll

	// Mettre à jour Redis
	updatedUserJSON, err := json.Marshal(userRedis)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize user"})
		return
	}
	err = database.RedisClient.Set(database.Ctx, userKey, updatedUserJSON, 72*time.Hour).Err()
	if err != nil {
		fmt.Printf("Failed to update user in Redis: %v\n", err)
	}

	// Version simple : mise à jour directe
	if err := database.DB.Model(&models.User{}).Where("id = ? AND free_chips_amount_bankroll >= ?", userID, input.Amount).
		Update("free_chips_amount_bankroll", gorm.Expr("free_chips_amount_bankroll - ?", input.Amount)).Error; err != nil {
		// Log l'erreur mais continue (car Redis est à jour)
		fmt.Printf("Failed to update PostgreSQL bankroll: %v\n", err)
	}

	// ---------- 4. Asseoir l'utilisateur sur le siège ----------
	table.Seats[seatIndex].UserID = int(userID.(uint))

	if table.DealerSeatIndex == -1 {
		table.DealerSeatIndex = seatIndex
	}
	table.Seats[seatIndex].AmountAtStake = input.Amount
	table.SeatsConnected[seatIndex] = true

	occupiedSeats := 0
	for _, seat := range table.Seats {
		if seat.UserID != 0 {
			occupiedSeats++
		}
	}

	// Dans SitAtTable, après avoir assis l'utilisateur et compté occupiedSeats

	if occupiedSeats >= 2 && !table.GameStarted && !table.Starting {
		table.Starting = true
		updatedJSON, _ := json.Marshal(table)
		database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)

		// Lancer la distribution des cartes dans une goroutine
		go func() {
			time.Sleep(3 * time.Second) // Attendre 3 secondes pour le toast
			DistributeCardsForHand(tableID)
		}()

		// Envoyer le message GAME_STARTING pour le toast
		database.PublishGameStarting(tableID)
	}

	// Sauvegarder la table mise à jour
	updatedJSON, err := json.Marshal(table)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize table"})
		return
	}
	err = database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save table"})
		return
	}

	// Notifier les clients
	database.PublishTableUpdate(tableID)

	c.JSON(http.StatusOK, gin.H{"message": "Seated successfully", "table": table})
}

func UnseatFromTable(c *gin.Context) {
	tableID := c.Param("id")
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	key := "table:" + tableID
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		return
	}

	var table models.PlayingTable
	if err := json.Unmarshal([]byte(val), &table); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse table data"})
		return
	}

	// Chercher le siège occupé par l'utilisateur
	var seatAmount int
	seatIndex := -1
	for i, seat := range table.Seats {
		if seat.UserID == int(userID.(uint)) {
			seatAmount = seat.AmountAtStake
			seatIndex = i
			break
		}
	}
	if seatIndex == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not seated at this table"})
		return
	}

	// Restituer le montant misé à la bankroll de l'utilisateur
	if seatAmount > 0 {
		// Récupérer l'utilisateur depuis Redis
		userKey := fmt.Sprintf("user:%d", userID)
		userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
		if err == nil {
			var userRedis models.UserRedis
			if err := json.Unmarshal([]byte(userVal), &userRedis); err == nil {
				if userRedis.FreeChipsAmountBankroll != nil {
					newBankroll := *userRedis.FreeChipsAmountBankroll + float64(seatAmount)
					userRedis.FreeChipsAmountBankroll = &newBankroll
					updatedUserJSON, _ := json.Marshal(userRedis)
					database.RedisClient.Set(database.Ctx, userKey, updatedUserJSON, 72*time.Hour)
				}
			}
		}
		// Mettre à jour PostgreSQL
		database.DB.Model(&models.User{}).Where("id = ?", userID).
			Update("free_chips_amount_bankroll", gorm.Expr("free_chips_amount_bankroll + ?", seatAmount))
	}

	// Libérer le siège
	table.Seats[seatIndex].UserID = 0
	table.Seats[seatIndex].AmountAtStake = 0
	table.SeatsConnected[seatIndex] = false

	// Sauvegarder la table
	SaveAndNotify(&table)

	// Après avoir retiré le joueur et sauvegardé
	if table.GameStarted && table.CurrentTurnSeatIndex == seatIndex {
		advanceTurnAfterLeave(&table)
	}

	// Si une distribution est en cours, on annule
	if table.IsDealing {
		table.DistributionCancelled = true
		SaveAndNotify(&table)
		// Ne pas appeler checkAndHandleHandEndOnLeave ici, ce sera fait par DistributeCardsForHand
	}

	// Vérifier la fin de manche
	checkAndHandleHandEndOnLeave(&table)

	if table.DealerSeatIndex == seatIndex {
		newDealer := -1
		for i, seat := range table.Seats {
			if seat.UserID != 0 && i != seatIndex {
				newDealer = i
				break
			}
		}
		table.DealerSeatIndex = newDealer
	}

	occupiedSeats := 0
	for _, seat := range table.Seats {
		if seat.UserID != 0 {
			occupiedSeats++
		}
	}
	if occupiedSeats < 2 {
		table.GameStarted = false
		table.Starting = false
		table.Deck = []string{}
		for i := range table.SeatCards {
			table.SeatCards[i].Hand = []string{}
			table.SeatCards[i].Played = []string{}
		}
	}

	usernames, _ := fetchUsernames(table.Players)
	table.PlayerUsernames = usernames

	// Sauvegarder la table
	updatedJSON, err := json.Marshal(table)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize table"})
		return
	}
	err = database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save table"})
		return
	}

	database.PublishTableUpdate(tableID)
	c.JSON(http.StatusOK, gin.H{"message": "Unseated successfully", "table": table})
}

// fetchUsernames retourne les noms des joueurs à partir de leurs IDs (depuis Redis)
func fetchUsernames(userIDs []uint) ([]string, error) {
	usernames := make([]string, len(userIDs))
	for i, id := range userIDs {
		key := fmt.Sprintf("user:%d", id)
		val, err := database.RedisClient.Get(database.Ctx, key).Result()
		if err != nil {
			usernames[i] = fmt.Sprintf("Joueur %d", id)
			continue
		}
		var user models.UserRedis
		if err := json.Unmarshal([]byte(val), &user); err != nil {
			usernames[i] = fmt.Sprintf("Joueur %d", id)
			continue
		}
		usernames[i] = user.Username
	}
	return usernames, nil
}

func createDeck() []string {
	suits := []string{"c", "d", "h", "s"}
	values := []string{"3", "4", "5", "6", "7", "8", "9", "10"}
	deck := []string{}
	for _, suit := range suits {
		for _, value := range values {
			deck = append(deck, suit+value)
		}
	}
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	return deck
}

// advanceTurnAfterLeave avance le tour si le joueur actuel a quitté
func advanceTurnAfterLeave(table *models.PlayingTable) {
	// Si aucune manche en cours ou pas de tour, on ne fait rien
	if !table.GameStarted || table.CurrentRound == 0 {
		return
	}

	// Cas où le joueur se lève alors que ce n'est pas son tour : on ne fait rien (le tour reste sur le joueur suivant déjà)
	// Mais on vérifie : si le joueur actuel est vide, on doit avancer.
	seatIdx := table.CurrentTurnSeatIndex
	if seatIdx < 0 || seatIdx >= len(table.Seats) || table.Seats[seatIdx].UserID != 0 {
		// Le joueur actuel est toujours là, rien à faire
		return
	}

	// Si la couleur demandée est non vide, ou que c'est le round 1, on utilise la méthode normale (prochain siège avec des cartes)
	if table.SuitRequired != "" || table.CurrentRound == 1 {
		nextPlayer := getNextSeatIndexInTurn(table, seatIdx)
		if nextPlayer != -1 {
			table.CurrentTurnSeatIndex = nextPlayer
			SaveAndNotify(table)
			return
		}
		// Sinon, plus de joueur, on finit la manche
		handleNoActivePlayers(table)
		return
	}

	// Cas : round > 1 et SuitRequired vide (tour pas encore commencé)
	// On doit trouver le joueur qui avait la deuxième meilleure carte au round précédent
	if len(table.RoundHistory) >= 2 {
		prevRound := table.RoundHistory[len(table.RoundHistory)-1] // dernier round enregistré
		// Récupérer les cartes du round précédent pour chaque siège
		// On a prevRound.PlayedCards qui est un []RoundCard avec SeatIndex et Card
		// On doit trouver le meilleur (celui qui a gagné) et le deuxième meilleur
		// Le meilleur est prevRound.WinnerSeat, qui est le joueur parti.
		// On cherche le deuxième meilleur parmi les autres.

		// Filtrer les cartes des autres joueurs (différents de winnerSeat)
		var bestCard string
		var secondBestSeat int
		for _, played := range prevRound.PlayedCards {
			if played.SeatIndex == prevRound.WinnerSeat {
				continue
			}
			// Comparer selon la couleur (SuitRequired) du round précédent
			if cardSuit(played.Card) == prevRound.SuitRequired {
				if bestCard == "" || CardValue(played.Card) > CardValue(bestCard) {
					bestCard = played.Card
					secondBestSeat = played.SeatIndex
				}
			}
		}
		// Fallback : si personne n'a de carte de la couleur, prendre le premier joueur disponible
		if secondBestSeat == -1 {
			nextPlayer := getNextSeatIndexInTurn(table, seatIdx)
			if nextPlayer != -1 {
				table.CurrentTurnSeatIndex = nextPlayer
				SaveAndNotify(table)
				return
			}
			handleNoActivePlayers(table)
			return
		}

		// Vérifier que ce joueur est toujours participant (a des cartes)
		if len(table.SeatCards[secondBestSeat].Hand) == 0 {
			// Il n'a plus de cartes, on passe au suivant
			nextPlayer := getNextSeatIndexInTurn(table, seatIdx)
			if nextPlayer != -1 {
				table.CurrentTurnSeatIndex = nextPlayer
				SaveAndNotify(table)
				return
			}
			handleNoActivePlayers(table)
			return
		}

		table.CurrentTurnSeatIndex = secondBestSeat
		SaveAndNotify(table)
		log.Printf("Tour attribué au deuxième meilleur du round précédent (siège %d)", secondBestSeat)
		return
	}

	// Fallback : si pas d'historique, on utilise la méthode normale
	nextPlayer := getNextSeatIndexInTurn(table, seatIdx)
	if nextPlayer != -1 {
		table.CurrentTurnSeatIndex = nextPlayer
		SaveAndNotify(table)
		return
	}
	handleNoActivePlayers(table)
}
