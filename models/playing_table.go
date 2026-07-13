package models

import (
	"time"
)

type Seat struct {
	UserID        int `json:"user_id"`
	AmountAtStake int `json:"amount_at_stake"`
}

type SeatCards struct {
	Hand   []string `json:"hand"`
	Played []string `json:"played"`
}

type ChatMessage struct {
	ID        string    `json:"id"`
	UserID    uint      `json:"user_id"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type RoundCard struct {
	SeatIndex int    `json:"seat_index"`
	Card      string `json:"card"`
}

// PendingSitRequest stocke une demande d’asseoir en attente
type PendingSitRequest struct {
	UserID    uint  `json:"userID"`
	SeatIndex int   `json:"seatIndex"`
	Amount    int   `json:"amount"`
	Timestamp int64 `json:"timestamp"`
}

type RoundHistoryEntry struct {
	RoundNumber  int         `json:"roundNumber"`
	SuitRequired string      `json:"suitRequired"`
	PlayedCards  []RoundCard `json:"playedCards"`
	WinnerSeat   int         `json:"winnerSeat"`
	WinnerCard   string      `json:"winnerCard"`
}

type PlayingTable struct {
	ID                    string              `json:"id"`
	Name                  string              `json:"name"` // Nouveau champ
	CreatedBy             uint                `json:"created_by"`
	IsPrivate             bool                `json:"is_private"`
	IsRealMoney           bool                `json:"is_real_money"`
	Paid33                bool                `json:"paid_33"`
	Bet                   int                 `json:"bet"`
	Status                string              `json:"status"`
	Players               []uint              `json:"players"`
	PlayerUsernames       []string            `json:"player_usernames"`
	CreatedAt             time.Time           `json:"created_at"`
	Seats                 []Seat              `json:"seats"`
	SeatsConnected        []bool              `json:"seatsConnected"` // true = connecté, false = déconnecté
	SeatCards             []SeatCards         `json:"seatCards"`      // 4 éléments (un par siège)
	DealerSeatIndex       int                 `json:"dealer_seat_index"`
	Turn                  string              `json:"turn"`
	LastWinningSeat       string              `json:"last_winning_seat"` // Pour garder une trace du dernier gagnant
	LastRoundWinner       string              `json:"last_round_winner"`
	ThreeSevenSeat        int                 `json:"three_seven_seat"`
	Pot                   int                 `json:"pot"`
	HandOver              bool                `json:"hand_over"`
	HandCompleted         bool                `json:"hand_completed"` // Pour éviter les doubles démarrages de main
	WinMessages           []string            `json:"win_messages"`
	GameNotifications     []string            `json:"game_notifications"`
	History               []string            `json:"history"`
	SeatTurnTimer         []string            `json:"seat_turn_timer"`     // Timer pour le tour actuel
	DemandedSuit          []string            `json:"demanded_suit"`       // Couleur demandée pour le tour actuel
	CurrentRoundCards     []string            `json:"current_round_cards"` // Cartes jouées dans le tour actuel
	RoundNumber           int                 `json:"round_number"`
	CountHand             int                 `json:"count_hand"`         // Numéro du tour actuel (1-5)
	HandParticipants      []string            `json:"hand_participants"`  // Mémoire tampon des joueurs qui participent à la main en cours
	WonByCombination      bool                `json:"won_by_combination"` // Flag pour indiquer une victoire par combinaison
	OnTurnChanged         []string            `json:"on_turn_changed"`    // Callback pour notifier du changement de tour
	ChatRoom              []string            `json:"chat_room"`
	ChatMessages          []ChatMessage       `json:"chat_messages"`
	InviteLink            string              `json:"invite_link"`
	DisconnectedAt        []int64             `json:"disconnected_at"` // 0 = connecté
	Deck                  []string            `json:"deck"`
	GameStarted           bool                `json:"game_started"`
	Starting              bool                `json:"starting"`
	Dealing               bool                `json:"dealing"`
	CurrentRound          int                 `json:"current_round"`           // 0 = pas encore commencé, 1..5
	CurrentTurnSeatIndex  int                 `json:"current_turn_seat_index"` // -1 si aucun
	SuitRequired          string              `json:"suit_required"`           // "" si pas définie
	RoundPlayedCards      []RoundCard         `json:"round_played_cards"`
	RoundWinnerSeatIndex  int                 `json:"round_winner_seat_index"`
	LastRoundWinnerSeat   int                 `json:"last_round_winner_seat"`
	HandWinnerSeat        int                 `json:"hand_winner_seat"`
	RevealedSeats         []bool              `json:"revealedSeats"`
	ParticipatingSeats    []bool              `json:"participatingSeats"` // true pour les sièges participants à la manche
	PausedSeats           []bool              `json:"pausedSeats"`
	IsDealing             bool                `json:"isDealing"` // Vrai si une distribution est en cours
	DistributionCancelled bool                `json:"distributionCancelled"`
	RoundHistory          []RoundHistoryEntry `json:"roundHistory"`
	WaitingList           []uint              `json:"waitingList"`
	WaitingListUsernames  []string            `json:"waitingListUsernames,omitempty"`
}
