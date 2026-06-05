package main

import (
	"encoding/json"
	"lekatika-server/controllers"
	"lekatika-server/database"
	"lekatika-server/middleware" // À créer
	"lekatika-server/utils"
	"time"

	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func main() {
	// Connexion à PostgreSQL
	database.Connect()
	// Connexion à Redis
	database.ConnectRedis()

	hub := NewHub()
	go hub.Run()
	go hub.subscribeToRedis() // <-- nouvelle goroutine

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

	router.GET("/ws", func(c *gin.Context) {
		serveWs(hub, c)
	})

	// Routes protégées par JWT
	protected := router.Group("/api")
	protected.Use(middleware.AuthMiddleware())
	{
		protected.GET("/user/me", controllers.GetCurrentUser)
		protected.POST("/tables", controllers.CreateTable)
		protected.GET("/tables/:id", controllers.GetTable)
		protected.POST("/join/:id", controllers.JoinTable)
		protected.GET("/tables", controllers.ListTables)
		protected.DELETE("/tables/:id/leave", controllers.LeaveTable)
		protected.POST("/logout", controllers.Logout)
		protected.POST("/tables/:id/sit/:seatId", controllers.SitAtTable)
		protected.POST("/tables/:id/unseat", controllers.UnseatFromTable)
	}

	router.Run("localhost:8080")
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // A configurer pour la production
}

func serveWs(hub *Hub, c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}
	userID, err := utils.ValidateToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := &Client{
		conn:   conn,
		send:   make(chan []byte, 256),
		userID: userID,
	}
	hub.register <- client

	// Envoyer les tables à reconnecter
	tableIDs, err := controllers.GetTablesForUser(userID)
	if err == nil && len(tableIDs) > 0 {
		msg := map[string]interface{}{
			"type":     "RECONNECT_TABLES",
			"tableIds": tableIDs,
		}
		data, _ := json.Marshal(msg)
		client.send <- data
	}

	go client.writePump()
	go client.readPump(hub)
}
