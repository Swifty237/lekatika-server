package main

import (
	"lekatika-server/controllers"
	"lekatika-server/database"

	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Connexion à la base de données
	database.Connect()

	// Initialisation du routeur Gin
	router := gin.Default()

	// Configuration CORS (à placer avant vos routes)
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"}, // Votre frontend (modifiable selon vos besoins)
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Groupe de routes d'authentification
	authGroup := router.Group("/api/auth")
	{
		authGroup.POST("/register", controllers.Register)
		authGroup.POST("/login", controllers.Login)
	}

	// Démarrage du serveur
	router.Run("localhost:8080")
}
