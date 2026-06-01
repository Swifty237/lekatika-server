package main

import (
	"lekatika-server/controllers"
	"lekatika-server/database"
	"lekatika-server/middleware" // À créer
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Connexion à PostgreSQL
	database.Connect()
	// Connexion à Redis
	database.ConnectRedis()

	router := gin.Default()

	// CORS (inchangé)
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Routes publiques (authentification)
	authGroup := router.Group("/api/auth")
	{
		authGroup.POST("/register", controllers.Register)
		authGroup.POST("/login", controllers.Login)
	}

	// Routes protégées par JWT
	protected := router.Group("/api")
	protected.Use(middleware.AuthMiddleware())
	{
		protected.GET("/user/me", controllers.GetCurrentUser)
		protected.POST("/tables", controllers.CreateTable)
		protected.GET("/tables/:id", controllers.GetTable)
		protected.POST("/join/:id", controllers.JoinTable)
		protected.GET("/tables", controllers.ListTables)
	}

	router.Run("localhost:8080")
}
