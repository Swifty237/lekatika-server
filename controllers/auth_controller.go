package controllers

import (
	"errors"
	"fmt"
	"lekatika-server/database"
	"lekatika-server/models"
	"net/http"
	"os"
	"time"

	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type RegisterInput struct {
	Username                string   `json:"username" binding:"required"`
	Email                   string   `json:"email" binding:"required,email"`
	Password                string   `json:"password" binding:"required,min=6"`
	FreeChipsAmountBankroll *float64 `json:"free_chips_amount_bankroll,omitempty"`
}

type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func Register(c *gin.Context) {
	var input RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1. Recherche de l'utilisateur (même soft-deleted) par username OU email
	var existingUser models.User
	err := database.DB.Unscoped().Where("username = ? OR email = ?", input.Username, input.Email).First(&existingUser).Error

	if err == nil {
		// Un enregistrement existe (actif ou supprimé)
		if existingUser.DeletedAt.Valid {
			// 2. Le compte est soft-deleted → on le restaure
			existingUser.DeletedAt = gorm.DeletedAt{} // remet à NULL

			// Mise à jour des informations (on écrase avec les nouvelles données)
			existingUser.Username = input.Username
			existingUser.Email = input.Email
			if input.FreeChipsAmountBankroll != nil {
				existingUser.FreeChipsAmountBankroll = input.FreeChipsAmountBankroll
			}
			// Mise à jour du mot de passe
			if err := existingUser.HashPassword(input.Password); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
				return
			}
			// Sauvegarde en base (supprime le soft delete)
			if err := database.DB.Save(&existingUser).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore user"})
				return
			}

			// 3. Mettre à jour le cache Redis (optionnel mais recommandé)
			//    On peut synchroniser directement ou laisser le login le faire plus tard.
			//    Pour éviter un état incohérent, on le fait ici.
			syncUserToRedis(existingUser)

			c.JSON(http.StatusOK, gin.H{"message": "Account restored successfully"})
			return
		} else {
			// Compte actif → conflit
			c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
			return
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		// Autre erreur inattendue
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// 4. Aucun utilisateur trouvé (même supprimé) → création normale
	user := models.User{
		Username:                input.Username,
		Email:                   input.Email,
		FreeChipsAmountBankroll: input.FreeChipsAmountBankroll,
	}
	if err := user.HashPassword(input.Password); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}
	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// (Optionnel) on peut aussi ajouter l'utilisateur en cache Redis après création
	// mais cela sera fait lors du login.

	c.JSON(http.StatusOK, gin.H{"message": "Registration successful"})
}

// syncUserToRedis (déjà présente dans user_controller.go) est utilisée pour mettre à jour le cache.
// Elle doit être exportée ou accessible dans le package controllers.

func Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ? OR email = ?", input.Username, input.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username/email or password"})
		return
	}

	if err := user.CheckPassword(input.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username/email or password"})
		return
	}

	// Générer un token JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(time.Hour * 72).Unix(),
	})
	tokenString, err := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Créer l'objet UserRedis pour le cache
	userRedis := models.UserRedis{
		// hérite des champs de gorm.Model (ID, CreatedAt, UpdatedAt, DeletedAt)
		// mais nous n'avons pas ces champs dans la réponse JSON. On met juste l'ID.
		Model:                   gorm.Model{ID: user.ID},
		Username:                user.Username,
		Email:                   user.Email,
		FreeChipsAmountBankroll: user.FreeChipsAmountBankroll,
		RealChipsAmountBankroll: user.RealChipsAmountBankroll,
		ProfilePictureLink:      user.ProfilePictureLink,
		LastModification:        user.LastModification,
		PlayingTableIDs:         []string{},
		IsConnected:             user.IsConnected,
		Bio:                     user.Bio,
	}

	userJSON, err := json.Marshal(userRedis)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize user data"})
		return
	}

	// Clé Redis : "user:{id}"
	err = database.RedisClient.Set(database.Ctx, fmt.Sprintf("user:%d", user.ID), userJSON, 72*time.Hour).Err()
	if err != nil {
		fmt.Printf("Failed to store user in Redis: %v\n", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   tokenString,
		"user": gin.H{
			"user_id":                    user.ID,
			"username":                   user.Username,
			"email":                      user.Email,
			"free_chips_amount_bankroll": user.FreeChipsAmountBankroll,
			"real_chips_amount_bankroll": user.RealChipsAmountBankroll,
			"profile_picture_link":       user.ProfilePictureLink,
			"last_modification":          user.LastModification,
			"playing_table_ids":          userRedis.PlayingTableIDs,
			"isConnected":                user.IsConnected,
			"bio":                        user.Bio,
		},
	})
}
