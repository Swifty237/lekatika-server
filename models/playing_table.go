package models

import (
	"time"
)

type PlayingTable struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"` // Nouveau champ
	CreatedBy         uint      `json:"created_by"`
	IsPrivate         bool      `json:"is_private"`
	IsRealMoney       bool      `json:"is_real_money"`
	Bet               int       `json:"bet"`
	Status            string    `json:"status"`
	Players           []uint    `json:"players"`
	CreatedAt         time.Time `json:"created_at"`
	Seats             []uint    `json:"seats"`
	Dealer            string    `json:"dealer"`
	Turn              string    `json:"turn"`
	LastWinningSeat   string    `json:"lastWinningSeat"` // Pour garder une trace du dernier gagnant
	LastRoundWinner   string    `json:"lastRoundWinner"`
	Pot               string    `json:"pot"`
	HandOver          bool      `json:"handOver"`
	HandCompleted     bool      `json:"handCompleted"` // Pour éviter les doubles démarrages de main
	WinMessages       []string  `json:"winMessages"`
	GameNotifications []string  `json:"gameNotifications"`
	History           []string  `json:"history"`
	SeatTurnTimer     []string  `json:"seatTurnTimer"`     // Timer pour le tour actuel
	DemandedSuit      []string  `json:"demandedSuit"`      // Couleur demandée pour le tour actuel
	CurrentRoundCards []string  `json:"currentRoundCards"` // Cartes jouées dans le tour actuel
	RoundNumber       int       `json:"roundNumber"`
	CountHand         int       `json:"countHand"`        // Numéro du tour actuel (1-5)
	HandParticipants  []string  `json:"handParticipants"` // Mémoire tampon des joueurs qui participent à la main en cours
	WonByCombination  bool      `json:"wonByCombination"` // Flag pour indiquer une victoire par combinaison
	OnTurnChanged     []string  `json:"onTurnChanged"`    // Callback pour notifier du changement de tour
	ChatRoom          []string  `json:"chatRoom"`
	InviteLink        string    `json:"inviteLink"`
}
