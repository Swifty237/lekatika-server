package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetCurrentUser(c *gin.Context) {
	// Récupérer l'ID utilisateur depuis le middleware d'authentification
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Chercher dans Redis
	key := fmt.Sprintf("user:%d", userID)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		// Si l'utilisateur n'est pas dans Redis (clé absente ou expirée)
		c.JSON(http.StatusNotFound, gin.H{"error": "User data not found in cache"})
		return
	}

	// Désérialiser les données utilisateur
	var userData map[string]interface{}
	if err := json.Unmarshal([]byte(val), &userData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse user data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": userData})
}

func Logout(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Retirer l'utilisateur de toutes les tables (libérer sièges, restituer jetons, etc.)
	RemoveUserFromAllTables(userID.(uint))

	// Supprimer l'entrée Redis de l'utilisateur
	key := fmt.Sprintf("user:%d", userID)
	err := database.RedisClient.Del(database.Ctx, key).Err()
	if err != nil {
		fmt.Printf("Failed to delete user from Redis: %v\n", err)
		// On ne bloque pas la réponse, on log seulement
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}
