package database

import (
	"fmt"
	"lekatika-server/models"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func Connect() {
	// Charger .env (ignorer si absent en production)
	_ = godotenv.Load()

	var dsn string
	// Si DATABASE_URL est définie, l'utiliser directement (production)
	if url := os.Getenv("DATABASE_URL"); url != "" {
		dsn = url
	} else {
		// Sinon, construire avec les variables individuelles (développement local)
		dsn = fmt.Sprintf(
			"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Europe/Paris",
			os.Getenv("DB_HOST"),
			os.Getenv("DB_USER"),
			os.Getenv("DB_PASSWORD"),
			os.Getenv("DB_NAME"),
			os.Getenv("DB_PORT"),
		)
	}

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	DB = database
	// Migrer le schéma User
	DB.AutoMigrate(&models.User{})
	fmt.Println("Database connected & migrated")
}
