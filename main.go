package main

import (
	"encoding/json"
	"fmt"
	"lekatika-server/controllers"
	"lekatika-server/database"
	"lekatika-server/middleware" // À créer
	"lekatika-server/models"
	"lekatika-server/utils"
	"log"
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

	// Initialiser la liste des utilisateurs connectés depuis PostgreSQL
	var users []models.User
	database.DB.Where("is_connected = ?", true).Find(&users)
	for _, u := range users {
		// Ajouter l'ID de l'utilisateur dans le set Redis
		database.RedisClient.SAdd(database.Ctx, "online_users", fmt.Sprintf("%d", u.ID))
	}

	hub := NewHub()
	go hub.Run()
	go hub.subscribeToRedis() // <-- nouvelle goroutine

	controllers.SetTimerHub(hub)

	// Démarrer la goroutine de nettoyage des déconnexions
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			CheckDisconnectedTables()
		}
	}()

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

	// Servir les fichiers statiques du dossier uploads
	router.Static("/uploads", "./uploads")

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
		protected.POST("/user/profile-picture", controllers.UpdateProfilePicture)
		protected.GET("/users/:id", controllers.GetUserByID)
		protected.POST("/user/add-chips", controllers.AddChips)
		protected.GET("/users/search", controllers.SearchUsers)
		protected.GET("/online-users/count", controllers.GetOnlineUsersCount)
		protected.GET("/online-users/list", controllers.GetOnlineUsersList)
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

	// Marquer connecté
	controllers.MarkUserConnectedToTable(userID)

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

	// Envoyer l'état du timer pour chaque table
	for _, tid := range tableIDs {
		active, seatIdx, remaining := hub.GetTimerState(tid)
		if active {
			payload := map[string]interface{}{
				"type":      "TIMER_START",
				"tableId":   tid,
				"seatIndex": seatIdx,
				"remaining": remaining,
			}
			data, _ := json.Marshal(payload)
			client.send <- data
		}
	}

	go client.writePump()
	go client.readPump(hub)
}

func CheckDisconnectedTables() {
	keys, err := database.RedisClient.Keys(database.Ctx, "table:*").Result()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	for _, key := range keys {
		val, err := database.RedisClient.Get(database.Ctx, key).Result()
		if err != nil {
			continue
		}
		var table models.PlayingTable
		if err := json.Unmarshal([]byte(val), &table); err != nil {
			continue
		}
		updated := false
		for i, seat := range table.Seats {
			if seat.UserID != 0 && !table.SeatsConnected[i] && table.DisconnectedAt[i] != 0 && now-table.DisconnectedAt[i] > 60 {
				userID := uint(seat.UserID)
				log.Printf("Expelling player %d from table %s seat %d", userID, table.ID, i+1)
				table.Seats[i].UserID = 0
				table.SeatsConnected[i] = false
				table.DisconnectedAt[i] = 0
				// Retirer des players
				newPlayers := []uint{}
				for _, p := range table.Players {
					if p != userID {
						newPlayers = append(newPlayers, p)
					}
				}
				table.Players = newPlayers
				updated = true
			}
		}
		if updated {
			// Si plus aucun joueur, supprimer la table
			if len(table.Players) == 0 {
				database.RedisClient.Del(database.Ctx, key)
				database.PublishTablesReload()
				log.Printf("Table %s deleted because no players left", table.ID)
				continue
			}
			// Sauvegarder la table mise à jour
			updatedJSON, _ := json.Marshal(table)
			database.RedisClient.Set(database.Ctx, key, updatedJSON, 24*time.Hour)
			database.PublishTableUpdate(table.ID)
			database.PublishTablesReload() // ← force le rafraîchissement du lobby
			log.Printf("Table %s updated after expulsion", table.ID)
		}
	}
}
