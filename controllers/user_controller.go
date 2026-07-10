package controllers

import (
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

func UpdateProfilePicture(c *gin.Context) {
	// Récupérer l'ID de l'utilisateur depuis le token JWT
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Récupérer le fichier depuis la requête
	file, err := c.FormFile("profilePicture")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// Vérifier le type MIME (image uniquement)
	contentType := file.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File must be an image"})
		return
	}

	// Vérifier la taille (ex: 5 Mo)
	if file.Size > 5*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image must be less than 5 MB"})
		return
	}

	// Générer un nom de fichier unique
	ext := filepath.Ext(file.Filename)
	filename := fmt.Sprintf("%d_%d%s", userID, time.Now().UnixNano(), ext)
	filepath := filepath.Join("uploads", "profiles", filename)

	// Sauvegarder le fichier
	if err := c.SaveUploadedFile(file, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}

	// Construire l'URL publique
	profilePictureURL := fmt.Sprintf("/uploads/profiles/%s", filename)

	// Mettre à jour l'utilisateur dans Redis et PostgreSQL
	// Récupérer l'utilisateur depuis Redis
	userKey := fmt.Sprintf("user:%d", userID)
	userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
	if err != nil {
		// Fallback : récupérer depuis PostgreSQL
		var user models.User
		if err := database.DB.First(&user, userID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		// Mettre à jour le champ ProfilePictureLink
		user.ProfilePictureLink = &profilePictureURL
		database.DB.Save(&user)
		// Mettre à jour Redis
		userRedis := models.UserRedis{
			Model:              gorm.Model{ID: user.ID},
			Username:           user.Username,
			Email:              user.Email,
			ProfilePictureLink: user.ProfilePictureLink,
			// ... autres champs
		}
		userJSON, _ := json.Marshal(userRedis)
		database.RedisClient.Set(database.Ctx, userKey, userJSON, 72*time.Hour)
		c.JSON(http.StatusOK, gin.H{"message": "Profile picture updated", "url": profilePictureURL})
		return
	}

	var userRedis models.UserRedis
	if err := json.Unmarshal([]byte(userVal), &userRedis); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse user data"})
		return
	}

	// Mettre à jour le champ ProfilePictureLink
	userRedis.ProfilePictureLink = &profilePictureURL
	updatedUserJSON, _ := json.Marshal(userRedis)
	database.RedisClient.Set(database.Ctx, userKey, updatedUserJSON, 72*time.Hour)

	// Mettre à jour PostgreSQL
	database.DB.Model(&models.User{}).Where("id = ?", userID).Update("profile_picture_link", profilePictureURL)

	c.JSON(http.StatusOK, gin.H{"message": "Profile picture updated", "url": profilePictureURL})
}

func GetUserByID(c *gin.Context) {
	userID := c.Param("id")
	id, err := strconv.Atoi(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	// Récupérer depuis Redis d'abord
	key := fmt.Sprintf("user:%d", id)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err == nil {
		var userRedis models.UserRedis
		if err := json.Unmarshal([]byte(val), &userRedis); err == nil {
			c.JSON(http.StatusOK, gin.H{"user": userRedis})
			return
		}
	}

	// Fallback PostgreSQL
	var user models.User
	if err := database.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Convertir en UserRedis si besoin
	userRedis := models.UserRedis{
		Model:              gorm.Model{ID: user.ID},
		Username:           user.Username,
		Email:              user.Email,
		ProfilePictureLink: user.ProfilePictureLink,
		// ... autres champs
	}
	c.JSON(http.StatusOK, gin.H{"user": userRedis})
}
