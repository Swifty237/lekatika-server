package models

import (
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	// Champs existants
	Username string `gorm:"unique;not null" json:"username"`
	Email    string `gorm:"unique;not null" json:"email"`
	Password string `json:"-"` // jamais exposé en JSON

	// Nouveaux champs nullables (pointeurs)
	// SocketId                *string   `json:"socketId,omitempty"`                     // nul à la création
	FreeChipsAmountBankroll *float64  `json:"free_chips_amount_bankroll,omitempty"`    // entier avec virgule, nul
	RealChipsAmountBankroll *float64  `json:"real_chips_amount_bankroll,omitempty"`    // entier avec virgule, nul
	ProfilePictureLink      *string   `json:"profile_picture_link,omitempty"`          // nul
	LastModification        time.Time `gorm:"autoUpdateTime" json:"last_modification"` // mis à jour automatiquement
}

type UserRedis struct {
	gorm.Model
	// Champs existants
	Username string `gorm:"unique;not null" json:"username"`
	Email    string `gorm:"unique;not null" json:"email"`
	Password string `json:"-"` // jamais exposé en JSON

	// Nouveaux champs nullables (pointeurs)
	// SocketId                *string   `json:"socketId,omitempty"`                     // nul à la création
	FreeChipsAmountBankroll *float64  `json:"free_chips_amount_bankroll,omitempty"`    // entier avec virgule, nul
	RealChipsAmountBankroll *float64  `json:"real_chips_amount_bankroll,omitempty"`    // entier avec virgule, nul
	ProfilePictureLink      *string   `json:"profile_picture_link,omitempty"`          // nul
	LastModification        time.Time `gorm:"autoUpdateTime" json:"last_modification"` // mis à jour automatiquement
	PlayingTableIDs         []string  `json:"playing_table_ids"`
}

// HashPassword hashe le mot de passe
func (user *User) HashPassword(password string) error {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return err
	}
	user.Password = string(bytes)
	return nil
}

// CheckPassword vérifie le mot de passe
func (user *User) CheckPassword(providedPassword string) error {
	return bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(providedPassword))
}
