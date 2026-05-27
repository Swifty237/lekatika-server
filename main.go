package main

import (
	"lekatika-server/controllers"
	"lekatika-server/database"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Connexion à la base de données
	database.Connect()

	// 2. Initialisation du routeur Gin
	router := gin.Default()

	// 3. Définition des routes d'authentification
	authGroup := router.Group("/api/auth")
	{
		authGroup.POST("/register", controllers.Register)
		authGroup.POST("/login", controllers.Login)
	}

	// 4. Démarrage du serveur
	router.Run("localhost:8080")
}
