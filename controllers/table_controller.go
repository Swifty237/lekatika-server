package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

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
		ID:                tableID,
		Name:              input.Name, // ← utilisation
		CreatedBy:         userID.(uint),
		IsPrivate:         input.IsPrivate,
		IsRealMoney:       input.IsRealMoney,
		Paid33:            input.Paid33,
		Bet:               input.Bet,
		Status:            "waiting",
		Players:           []uint{userID.(uint)},
		PlayerUsernames:   []string{},
		CreatedAt:         time.Now(),
		Seats:             []models.Seat{seat1, seat2, seat3, seat4},
		SeatsConnected:    []bool{false, false, false, false}, // initialement tous déconnectés (siège vide)
		Dealer:            "",
		Turn:              "",
		LastWinningSeat:   "",
		LastRoundWinner:   "",
		Pot:               "0",
		HandOver:          false,
		HandCompleted:     false,
		WinMessages:       []string{},
		GameNotifications: []string{},
		History:           []string{},
		SeatTurnTimer:     []string{},
		DemandedSuit:      []string{},
		CurrentRoundCards: []string{},
		RoundNumber:       0,
		CountHand:         0,
		HandParticipants:  []string{},
		WonByCombination:  false,
		OnTurnChanged:     []string{},
		ChatRoom:          []string{},
		InviteLink:        inviteLink,
		DisconnectedAt:    []int64{0, 0, 0, 0},
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

	// 1. Libérer le siège si l'utilisateur était assis
	for i, seat := range table.Seats {
		if seat.UserID == int(userID.(uint)) {
			table.Seats[i].UserID = 0
			table.SeatsConnected[i] = false
			break
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
		err := database.RedisClient.Del(database.Ctx, key).Err()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete table"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Table deleted because no players left"})

		RemoveTableFromUser(userID.(uint), tableID)

		database.PublishTablesReload()
		database.PublishTableUpdate(tableID)
		return
	}

	// 4. Sauvegarder la table mise à jour (avec siège libéré)
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

	// Vérifier si l'utilisateur n'est pas déjà assis ailleurs
	for i, seat := range table.Seats {
		if seat.UserID == int(userID.(uint)) {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Already seated at seat %d", i+1)})
			return
		}
	}

	// Vérifier que l'indice du siège existe
	if seatIndex >= len(table.Seats) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Seat index out of range"})
		return
	}

	// Vérifier si le siège est libre
	if table.Seats[seatIndex].UserID != 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Seat already taken"})
		return
	}

	// Asseoir l'utilisateur
	table.Seats[seatIndex].UserID = int(userID.(uint))
	table.SeatsConnected[seatIndex] = true

	usernames, _ := fetchUsernames(table.Players)
	table.PlayerUsernames = usernames

	// Sauvegarder
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
	found := false
	for i, seat := range table.Seats {
		if seat.UserID == int(userID.(uint)) {
			table.Seats[i].UserID = 0
			table.SeatsConnected[i] = false

			usernames, _ := fetchUsernames(table.Players)
			table.PlayerUsernames = usernames

			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not seated at this table"})
		return
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

	// Notifier tous les clients (WebSocket) de la mise à jour
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
