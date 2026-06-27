package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"time"

	"gorm.io/gorm"
)

func AddTableToUser(userID uint, tableID string) {
	key := fmt.Sprintf("user:%d", userID)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return
	}
	var user models.UserRedis
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		return
	}
	for _, id := range user.PlayingTableIDs {
		if id == tableID {
			return
		}
	}
	user.PlayingTableIDs = append(user.PlayingTableIDs, tableID)
	updated, _ := json.Marshal(user)
	database.RedisClient.Set(database.Ctx, key, updated, 72*time.Hour)
}

func RemoveTableFromUser(userID uint, tableID string) {
	key := fmt.Sprintf("user:%d", userID)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return
	}
	var user models.UserRedis
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		return
	}
	newIDs := []string{}
	for _, id := range user.PlayingTableIDs {
		if id != tableID {
			newIDs = append(newIDs, id)
		}
	}
	user.PlayingTableIDs = newIDs
	updatedJSON, err := json.Marshal(user)
	if err != nil {
		return
	}
	database.RedisClient.Set(database.Ctx, key, updatedJSON, 72*time.Hour)
}

// RemoveUserFromAllTables retire un utilisateur de toutes les tables auxquelles il participe,
// libère son siège, restitue ses jetons, et supprime les tables vides.
func RemoveUserFromAllTables(userID uint) {
	// Récupérer la liste des tables de l'utilisateur
	tableIDs, err := GetTablesForUser(userID)
	if err != nil {
		return
	}
	if len(tableIDs) == 0 {
		return
	}

	for _, tableID := range tableIDs {
		key := "table:" + tableID
		val, err := database.RedisClient.Get(database.Ctx, key).Result()
		if err != nil {
			continue
		}
		var table models.PlayingTable
		if err := json.Unmarshal([]byte(val), &table); err != nil {
			continue
		}

		// Libérer le siège si occupé
		seatIndex := -1
		seatAmount := 0
		for i, seat := range table.Seats {
			if seat.UserID == int(userID) {
				seatAmount = seat.AmountAtStake
				seatIndex = i
				break
			}
		}
		if seatIndex != -1 {
			table.Seats[seatIndex].UserID = 0
			table.Seats[seatIndex].AmountAtStake = 0
			table.SeatsConnected[seatIndex] = false
		}

		// Retirer l'utilisateur de la liste des joueurs
		newPlayers := []uint{}
		for _, p := range table.Players {
			if p != userID {
				newPlayers = append(newPlayers, p)
			}
		}
		table.Players = newPlayers

		// Restituer les jetons si le joueur était assis
		if seatAmount > 0 {
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
			// Mise à jour PostgreSQL
			database.DB.Model(&models.User{}).Where("id = ?", userID).
				Update("free_chips_amount_bankroll", gorm.Expr("free_chips_amount_bankroll + ?", seatAmount))
		}

		// Mettre à jour les noms des joueurs (si nécessaire, pour le front)
		usernames, _ := fetchUsernames(table.Players)
		table.PlayerUsernames = usernames

		// Si la table est vide, la supprimer
		if len(table.Players) == 0 {
			database.RedisClient.Del(database.Ctx, key)
			database.PublishTablesReload()
			database.PublishTableUpdate(tableID)
		} else {
			// Sauvegarder la table mise à jour
			updatedJSON, err := json.Marshal(table)
			if err != nil {
				continue
			}
			database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
			database.PublishTablesReload()
			database.PublishTableUpdate(tableID)
		}

		// Mettre à jour le cache utilisateur (retirer cette table de sa liste)
		RemoveTableFromUser(userID, tableID)
	}
}
