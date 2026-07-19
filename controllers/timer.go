package controllers

type TimerHubInterface interface {
	StartTimer(tableID string, seatIndex int)
	StopTimer(tableID string)
	StartBreakTimer(tableID string, seatIndex int) // Ajout
	StopBreakTimer(tableID string, seatIndex int)  // Ajout
}

var TimerHub TimerHubInterface

func SetTimerHub(hub TimerHubInterface) {
	TimerHub = hub
}
