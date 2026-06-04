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
	Paid33            bool      `json:"paid_33"`
	Bet               int       `json:"bet"`
	Status            string    `json:"status"`
	Players           []uint    `json:"players"`
	CreatedAt         time.Time `json:"created_at"`
	Seats             []uint    `json:"seats"`
	Dealer            string    `json:"dealer"`
	Turn              string    `json:"turn"`
	LastWinningSeat   string    `json:"last_winning_seat"` // Pour garder une trace du dernier gagnant
	LastRoundWinner   string    `json:"last_round_winner"`
	Pot               string    `json:"pot"`
	HandOver          bool      `json:"hand_over"`
	HandCompleted     bool      `json:"hand_completed"` // Pour éviter les doubles démarrages de main
	WinMessages       []string  `json:"win_messages"`
	GameNotifications []string  `json:"game_notifications"`
	History           []string  `json:"history"`
	SeatTurnTimer     []string  `json:"seat_turn_timer"`     // Timer pour le tour actuel
	DemandedSuit      []string  `json:"demanded_suit"`       // Couleur demandée pour le tour actuel
	CurrentRoundCards []string  `json:"current_round_cards"` // Cartes jouées dans le tour actuel
	RoundNumber       int       `json:"round_number"`
	CountHand         int       `json:"count_hand"`         // Numéro du tour actuel (1-5)
	HandParticipants  []string  `json:"hand_participants"`  // Mémoire tampon des joueurs qui participent à la main en cours
	WonByCombination  bool      `json:"won_by_combination"` // Flag pour indiquer une victoire par combinaison
	OnTurnChanged     []string  `json:"on_turn_changed"`    // Callback pour notifier du changement de tour
	ChatRoom          []string  `json:"chat_room"`
	InviteLink        string    `json:"invite_link"`
}
