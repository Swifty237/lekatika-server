package database

import (
	"context"
	"encoding/json"
	"fmt"
	"lekatika-server/models"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client
var Ctx = context.Background()

func ConnectRedis() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using defaults")
	}

	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}
	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD") // laisser vide si pas de mot de passe

	RedisClient = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", redisHost, redisPort),
		Password: redisPassword,
		DB:       0,
	})

	// Tester la connexion
	_, err = RedisClient.Ping(Ctx).Result()
	if err != nil {
		log.Fatal("Failed to connect to Redis:", err)
	}
	fmt.Println("Redis connected successfully")
}

type TableEvent struct {
	Type  string              `json:"type"`
	Table models.PlayingTable `json:"table"`
}

func PublishTablesReload() {
	event := map[string]string{
		"type": "RELOAD_TABLES",
	}
	eventJSON, _ := json.Marshal(event)
	RedisClient.Publish(Ctx, "tables", eventJSON)
}

func PublishTableUpdate(tableID string) {
	event := map[string]string{
		"type":    "TABLE_UPDATED",
		"tableId": tableID,
	}
	eventJSON, _ := json.Marshal(event)
	RedisClient.Publish(Ctx, "tables", eventJSON)
}
