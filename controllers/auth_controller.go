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
)

type RegisterInput struct {
	Username        string   `json:"username" binding:"required"`
	Email           string   `json:"email" binding:"required,email"`
	Password        string   `json:"password" binding:"required,min=6"`
	FreeChipsAmount *float64 `json:"freeChipsAmount,omitempty"`
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
		Username:        input.Username,
		Email:           input.Email,
		FreeChipsAmount: input.FreeChipsAmount,
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

	// Stocker l'utilisateur dans Redis avec une expiration de 72h (comme le token)
	userData := map[string]interface{}{
		"id":                 user.ID,
		"username":           user.Username,
		"email":              user.Email,
		"freeChipsAmount":    user.FreeChipsAmount,
		"realChipsAmount":    user.RealChipsAmount,
		"profilePictureLink": user.ProfilePictureLink,
		"lastModification":   user.LastModification,
		"playingTableId":     user.PlayingTableID,
		"personalDetailsId":  user.PersonalDetailsID,
		"paymentDetailsId":   user.PaymentDetailsID,
	}

	userJSON, err := json.Marshal(userData)
	if err != nil {
		// On log l'erreur mais on ne bloque pas la connexion
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize user data"})
		return
	}

	// Clé Redis : "user:{id}"
	err = database.RedisClient.Set(database.Ctx, fmt.Sprintf("user:%d", user.ID), userJSON, 72*time.Hour).Err()
	if err != nil {
		// Log l'erreur mais continue (non bloquant)
		fmt.Printf("Failed to store user in Redis: %v\n", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   tokenString,
		"user": gin.H{
			"id":                 user.ID,
			"username":           user.Username,
			"email":              user.Email,
			"freeChipsAmount":    user.FreeChipsAmount,
			"realChipsAmount":    user.RealChipsAmount,
			"profilePictureLink": user.ProfilePictureLink,
			"lastModification":   user.LastModification,
			"playingTableId":     user.PlayingTableID,
			"personalDetailsId":  user.PersonalDetailsID,
			"paymentDetailsId":   user.PaymentDetailsID,
		},
	})
}
