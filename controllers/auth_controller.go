package controllers

import (
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

	// Vérifier si l'utilisateur existe déjà
	var existingUser models.User
	if err := database.DB.Where("email = ?", input.Email).First(&existingUser).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	// Créer un nouvel utilisateur
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

	c.JSON(http.StatusOK, gin.H{"message": "Registration successful"})
}

func Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", input.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	if err := user.CheckPassword(input.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
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
		PlayingTableIDs:         []string{}, // slice vide pour l'instant
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
		},
	})
}
