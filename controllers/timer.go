package controllers

type TimerHubInterface interface {
	StartTimer(tableID string, seatIndex int)
	StopTimer(tableID string)
}

var TimerHub TimerHubInterface

func SetTimerHub(hub TimerHubInterface) {
	TimerHub = hub
}
