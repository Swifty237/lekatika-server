package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"net/http"
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
		CreatedAt:         time.Now(),
		Seats:             []uint{},
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

	// Supprimer l'utilisateur de la liste des joueurs
	newPlayers := []uint{}
	for _, playerID := range table.Players {
		if playerID != userID.(uint) {
			newPlayers = append(newPlayers, playerID)
		}
	}
	table.Players = newPlayers

	// Si plus aucun joueur, supprimer la table
	if len(table.Players) == 0 {
		err := database.RedisClient.Del(database.Ctx, key).Err()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete table"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Table deleted because no players left"})

		// Notifier la mise à jour
		database.PublishTablesReload()
		return
	}

	// Mettre à jour la table dans Redis
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

	// Notifier la mise à jour
	database.PublishTablesReload()

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
