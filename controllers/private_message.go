package controllers

type PrivateMessageSender interface {
	SendPrivateMessage(userID uint, payload interface{})
}

var privateMessageSender PrivateMessageSender

func SetPrivateMessageSender(sender PrivateMessageSender) {
	privateMessageSender = sender
}

func SendPrivateMessageToUser(userID uint, payload interface{}) {
	if privateMessageSender != nil {
		privateMessageSender.SendPrivateMessage(userID, payload)
	}
}
