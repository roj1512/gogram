package examples

import (
	"fmt"

	"github.com/roj1512/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	// Create a new client
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})

	// Connect to the server
	if err := client.Connect(); err != nil {
		panic(err)
	}

	// Authenticate the client using the bot token
	if err := client.LoginBot(botToken); err != nil {
		panic(err)
	}

	// Add a message handler
	client.AddMessageHandler(telegram.OnNewMessage, func(message *telegram.NewMessage) error {
		var (
			err error
		)
		// Print the message
		fmt.Println(message.Marshal())

		// Send a message
		if message.IsPrivate() {
			_, err = message.Respond(message)
		}
		return err
	})

	client.AddMessageHandler("/start", func(message *telegram.NewMessage) error {
		message.Reply("Hello, I am a bot!")
		return nil
	})

	// Start polling
	client.Idle()
}
