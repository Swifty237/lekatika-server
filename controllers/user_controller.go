package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UpdateBioInput struct {
	Bio string `json:"bio"`
}

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
	MarkUserDisconnected(userID.(uint))

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
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	file, err := c.FormFile("profilePicture")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// Vérifications (taille, type MIME)...
	if file.Size > 5*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image must be less than 5 MB"})
		return
	}

	// Ouvrir le fichier
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()

	// Configurer le client S3 pour Tigris
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bucket := os.Getenv("BUCKET_NAME")

	svc, err := getS3Client(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create S3 client"})
		return
	}

	// --- SUPPRESSION DE L'ANCIENNE PHOTO ---
	var oldKey *string

	// Essayer de récupérer l'ancienne clé depuis Redis
	userKey := fmt.Sprintf("user:%d", userID)
	userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
	if err == nil {
		var existingUser models.UserRedis
		if json.Unmarshal([]byte(userVal), &existingUser) == nil {
			oldKey = existingUser.ProfilePictureKey
		}
	}

	// Fallback : récupérer depuis PostgreSQL
	if oldKey == nil || *oldKey == "" {
		var existingUser models.User
		if database.DB.First(&existingUser, userID).Error == nil {
			oldKey = existingUser.ProfilePictureKey
		}
	}

	// Supprimer l'ancienne image du bucket si elle existe
	if oldKey != nil && *oldKey != "" {
		_, delErr := svc.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(*oldKey),
		})
		if delErr != nil {
			log.Printf("Failed to delete old profile picture (%s): %v", *oldKey, delErr)
		} else {
			log.Printf("Old profile picture deleted: %s", *oldKey)
		}
	}
	// --- FIN SUPPRESSION ---

	// Générer un nom de fichier unique
	filename := fmt.Sprintf("profiles/%d_%d.jpg", userID, time.Now().UnixNano())

	// Upload vers Tigris
	_, err = svc.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(filename),
		Body:        src,
		ContentType: aws.String("image/jpeg"),
		// ACL:         "public-read",
	})
	if err != nil {
		log.Printf("Erreur PutObject : %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload file: " + err.Error()})
		return
	}

	// Construire l'URL publique
	// Pour Tigris, l'URL est : https://<bucket>.fly.dev/<key>
	profilePictureURL := fmt.Sprintf("https://%s.t3.tigrisfiles.io/%s", bucket, filename)

	// Mettre à jour l'utilisateur dans Redis et PostgreSQL
	// Récupérer l'utilisateur depuis Redis
	userKey = fmt.Sprintf("user:%d", userID)
	userVal, err = database.RedisClient.Get(database.Ctx, userKey).Result()
	if err != nil {
		// Fallback : récupérer depuis PostgreSQL
		var user models.User
		if err := database.DB.First(&user, userID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		// Mettre à jour le champ ProfilePictureLink
		user.ProfilePictureLink = &profilePictureURL
		user.ProfilePictureKey = &filename // Stocker la clé
		database.DB.Save(&user)
		// Mettre à jour Redis
		userRedis := models.UserRedis{
			Model:                   gorm.Model{ID: user.ID},
			Username:                user.Username,
			Email:                   user.Email,
			ProfilePictureLink:      user.ProfilePictureLink,
			ProfilePictureKey:       user.ProfilePictureKey,
			FreeChipsAmountBankroll: user.FreeChipsAmountBankroll,
			RealChipsAmountBankroll: user.FreeChipsAmountBankroll,
			LastModification:        user.LastModification,
			IsConnected:             user.IsConnected,
			Bio:                     user.Bio,
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
		Model:                   gorm.Model{ID: user.ID},
		Username:                user.Username,
		Email:                   user.Email,
		FreeChipsAmountBankroll: user.FreeChipsAmountBankroll,
		RealChipsAmountBankroll: user.RealChipsAmountBankroll,
		ProfilePictureLink:      user.ProfilePictureLink,
		ProfilePictureKey:       user.ProfilePictureKey,
		LastModification:        user.LastModification,
		PlayingTableIDs:         []string{},
		IsConnected:             user.IsConnected,
		Bio:                     user.Bio,
	}
	c.JSON(http.StatusOK, gin.H{"user": userRedis})
}

// GetUserByIDFromRedis retourne un utilisateur depuis Redis par son ID
func GetUserByIDFromRedis(userID uint) (models.UserRedis, error) {
	key := fmt.Sprintf("user:%d", userID)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err != nil {
		return models.UserRedis{}, err
	}
	var userRedis models.UserRedis
	if err := json.Unmarshal([]byte(val), &userRedis); err != nil {
		return models.UserRedis{}, err
	}
	return userRedis, nil
}

func AddChips(c *gin.Context) {
	// Récupérer l'ID de l'utilisateur depuis le token
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Lire le corps de la requête
	var req struct {
		Amount       int    `json:"amount"`
		CurrencyType string `json:"currencyType"` // "free" ou "real"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validation
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Amount must be positive"})
		return
	}

	// Limite pour les chips fictifs (ex: 10000)
	maxFreeChips := 10000
	if req.CurrencyType == "free" && req.Amount > maxFreeChips {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Maximum %d free chips per addition", maxFreeChips)})
		return
	}

	// Récupérer l'utilisateur depuis Redis
	userKey := fmt.Sprintf("user:%d", userID)
	userVal, err := database.RedisClient.Get(database.Ctx, userKey).Result()
	if err != nil {
		// Fallback PostgreSQL
		var user models.User
		if err := database.DB.First(&user, userID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		// Mettre à jour
		updateUserBankroll(&user, req.CurrencyType, req.Amount)
		database.DB.Save(&user)
		// Synchroniser Redis
		syncUserToRedis(user)
		c.JSON(http.StatusOK, gin.H{"message": "Chips added successfully"})
		return
	}

	var userRedis models.UserRedis
	if err := json.Unmarshal([]byte(userVal), &userRedis); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse user data"})
		return
	}

	// Mettre à jour selon le type
	switch req.CurrencyType {
	case "free":
		if userRedis.FreeChipsAmountBankroll == nil {
			userRedis.FreeChipsAmountBankroll = new(float64)
		}
		// Vérifier la limite totale (ex: 3000 max total)
		if *userRedis.FreeChipsAmountBankroll+float64(req.Amount) > float64(maxFreeChips) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Total free chips cannot exceed %d", maxFreeChips)})
			return
		}
		*userRedis.FreeChipsAmountBankroll += float64(req.Amount)
	case "real":
		if userRedis.RealChipsAmountBankroll == nil {
			userRedis.RealChipsAmountBankroll = new(float64)
		}
		*userRedis.RealChipsAmountBankroll += float64(req.Amount)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid currency type"})
		return
	}

	// Sauvegarder Redis
	updatedUserJSON, _ := json.Marshal(userRedis)
	database.RedisClient.Set(database.Ctx, userKey, updatedUserJSON, 72*time.Hour)

	// Mettre à jour PostgreSQL
	var user models.User
	database.DB.First(&user, userID)
	updateUserBankroll(&user, req.CurrencyType, req.Amount)
	database.DB.Save(&user)

	c.JSON(http.StatusOK, gin.H{"message": "Chips added successfully"})
}

// Fonctions utilitaires
func updateUserBankroll(user *models.User, currencyType string, amount int) {
	switch currencyType {
	case "free":
		if user.FreeChipsAmountBankroll == nil {
			user.FreeChipsAmountBankroll = new(float64)
		}
		*user.FreeChipsAmountBankroll += float64(amount)
		return
	case "real":
		if user.RealChipsAmountBankroll == nil {
			user.RealChipsAmountBankroll = new(float64)
		}
		*user.RealChipsAmountBankroll += float64(amount)
		return
	default:
		return
	}
}

func syncUserToRedis(user models.User) {
	userRedis := models.UserRedis{
		Model:                   gorm.Model{ID: user.ID},
		Username:                user.Username,
		Email:                   user.Email,
		FreeChipsAmountBankroll: user.FreeChipsAmountBankroll,
		RealChipsAmountBankroll: user.RealChipsAmountBankroll,
		ProfilePictureLink:      user.ProfilePictureLink,
		ProfilePictureKey:       user.ProfilePictureKey,
		LastModification:        user.LastModification,
		PlayingTableIDs:         []string{},
		IsConnected:             user.IsConnected,
		Bio:                     user.Bio,
	}
	userJSON, _ := json.Marshal(userRedis)
	key := fmt.Sprintf("user:%d", user.ID)
	database.RedisClient.Set(database.Ctx, key, userJSON, 72*time.Hour)
}

func SearchUsers(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing search query"})
		return
	}

	var users []models.User
	// Recherche insensible à la casse, limite à 10 résultats
	if err := database.DB.Where("username ILIKE ?", "%"+query+"%").Limit(10).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Search failed"})
		return
	}

	results := []gin.H{}
	for _, u := range users {
		results = append(results, gin.H{
			"id":                   u.ID,
			"username":             u.Username,
			"profile_picture_link": u.ProfilePictureLink,
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": results})
}

// MarkUserConnected met à jour le statut de l'utilisateur à connecté
func MarkUserConnected(userID uint) {
	// Mettre à jour PostgreSQL
	database.DB.Model(&models.User{}).Where("id = ?", userID).Update("is_connected", true)
	// Mettre à jour Redis
	key := fmt.Sprintf("user:%d", userID)
	// Récupérer l'utilisateur depuis Redis, mettre à jour IsConnected
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err == nil {
		var userRedis models.UserRedis
		json.Unmarshal([]byte(val), &userRedis)
		userRedis.IsConnected = true
		updatedJSON, _ := json.Marshal(userRedis)
		database.RedisClient.Set(database.Ctx, key, updatedJSON, 72*time.Hour)
	}
	// Ajouter l'ID au set Redis des connectés
	database.RedisClient.SAdd(database.Ctx, "online_users", fmt.Sprintf("%d", userID))
}

// MarkUserDisconnected met à jour le statut de l'utilisateur à déconnecté
func MarkUserDisconnected(userID uint) {
	// Mettre à jour PostgreSQL
	database.DB.Model(&models.User{}).Where("id = ?", userID).Update("is_connected", false)
	// Mettre à jour Redis (user cache)
	key := fmt.Sprintf("user:%d", userID)
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err == nil {
		var userRedis models.UserRedis
		json.Unmarshal([]byte(val), &userRedis)
		userRedis.IsConnected = false
		updatedJSON, _ := json.Marshal(userRedis)
		database.RedisClient.Set(database.Ctx, key, updatedJSON, 72*time.Hour)
	}
	// Retirer l'ID du set Redis des connectés
	database.RedisClient.SRem(database.Ctx, "online_users", fmt.Sprintf("%d", userID))
}

func GetOnlineUsersCount(c *gin.Context) {
	count, err := database.RedisClient.SCard(database.Ctx, "online_users").Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get online users"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"online": count})
}

func GetOnlineUsersList(c *gin.Context) {
	// Récupérer les IDs
	ids, err := database.RedisClient.SMembers(database.Ctx, "online_users").Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get online users"})
		return
	}
	// Optionnel : récupérer les usernames
	var users []gin.H
	for _, idStr := range ids {
		id, _ := strconv.Atoi(idStr)
		// Récupérer depuis Redis ou PostgreSQL
		userKey := fmt.Sprintf("user:%d", id)
		val, err := database.RedisClient.Get(database.Ctx, userKey).Result()
		if err == nil {
			var userRedis models.UserRedis
			json.Unmarshal([]byte(val), &userRedis)
			users = append(users, gin.H{
				"id":       id,
				"username": userRedis.Username,
				// autres infos
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

func UpdateBio(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var input UpdateBioInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Mettre à jour PostgreSQL
	if err := database.DB.Model(&models.User{}).Where("id = ?", userID).Update("bio", input.Bio).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update bio"})
		return
	}

	// Mettre à jour Redis
	key := fmt.Sprintf("user:%d", userID)
	var userRedis models.UserRedis
	val, err := database.RedisClient.Get(database.Ctx, key).Result()
	if err == nil {
		json.Unmarshal([]byte(val), &userRedis)
		userRedis.Bio = input.Bio
		updatedJSON, _ := json.Marshal(userRedis)
		database.RedisClient.Set(database.Ctx, key, updatedJSON, 72*time.Hour)
	} else {
		// Fallback : récupérer depuis PostgreSQL et mettre à jour Redis
		var user models.User
		database.DB.First(&user, userID)
		syncUserToRedis(user)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Bio updated successfully", "bio": input.Bio})
}

// getS3Client retourne un client S3 configuré à partir des variables d'environnement
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION, AWS_ENDPOINT_URL_S3)
func getS3Client(ctx context.Context) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg), nil
}

// DeleteAccount supprime le compte de l'utilisateur si son solde réel est à 0
func DeleteAccount(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	id := userID.(uint)

	// Récupérer l'utilisateur
	var user models.User
	if err := database.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Vérifier que le solde réel est à 0
	if user.RealChipsAmountBankroll != nil && *user.RealChipsAmountBankroll != 0 {
		c.JSON(http.StatusConflict, gin.H{
			"error": "Impossible de supprimer le compte : le solde en points réels doit être de 0",
		})
		return
	}

	// Supprimer l'image du bucket si une clé existe
	if user.ProfilePictureKey != nil && *user.ProfilePictureKey != "" {
		ctx := context.Background()
		bucket := os.Getenv("BUCKET_NAME")

		svc, err := getS3Client(ctx)
		if err == nil {
			_, err = svc.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(*user.ProfilePictureKey),
			})
			if err != nil {
				log.Printf("Failed to delete profile picture from bucket: %v", err)
			} else {
				log.Printf("Profile picture deleted: %s", *user.ProfilePictureKey)
			}
		} else {
			log.Printf("Failed to create S3 client: %v", err)
		}
	}

	// Retirer l'utilisateur de toutes les tables
	RemoveUserFromAllTables(id)

	// Retirer du cache Redis
	redisKey := fmt.Sprintf("user:%d", id)
	database.RedisClient.Del(database.Ctx, redisKey)

	// Retirer du set des utilisateurs connectés
	database.RedisClient.SRem(database.Ctx, "online_users", fmt.Sprintf("%d", id))

	// Supprimer le compte de la base de données
	if err := database.DB.Delete(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur lors de la suppression"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Compte supprimé avec succès"})
}
