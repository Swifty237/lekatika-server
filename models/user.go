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
	SocketId           *string   `json:"socketId,omitempty"`                     // nul à la création
	FreeChipsAmount    *float64  `json:"freeChipsAmount,omitempty"`              // entier avec virgule, nul
	RealChipsAmount    *float64  `json:"realChipsAmount,omitempty"`              // entier avec virgule, nul
	ProfilePictureLink *string   `json:"profilePictureLink,omitempty"`           // nul
	LastModification   time.Time `gorm:"autoUpdateTime" json:"lastModification"` // mis à jour automatiquement

	// Clés étrangères pour les relations (valeurs nulles tant que non définies)
	PlayingTableID    *uint `json:"playingTableId,omitempty"`
	PersonalDetailsID *uint `json:"personalDetailsId,omitempty"`
	PaymentDetailsID  *uint `json:"paymentDetailsId,omitempty"`

	// Relations (à décommenter quand les modèles existent)
	// PlayingTable      *PlayingTable   `gorm:"foreignKey:PlayingTableID"`
	// PersonalDetails   *PersonalDetail `gorm:"foreignKey:PersonalDetailsID"`
	// PaymentDetails    *PaymentDetail  `gorm:"foreignKey:PaymentDetailsID"`
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
