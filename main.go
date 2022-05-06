package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	corev2 "github.com/sensu/sensu-go/api/core/v2"

	"github.com/sensu-community/sensu-plugin-sdk/sensu"
)

// HandlerConfig contains the Slack handler configuration
type HandlerConfig struct {
	sensu.PluginConfig
	rhoseURL     string
	clientId     string
	clientSecret string
}

const (
	webHookURL   = "webhook-url"
	clientId     = "client-id"
	clientSecret = "client-secret"
)

var (
	config = HandlerConfig{
		PluginConfig: sensu.PluginConfig{
			Name:     "sensu-rhose-handler",
			Short:    "Sensu handler plugin for Red Hat Openshift Smart Event",
			Keyspace: "sensu.io/plugins/sensu-rhose-handler/config",
		},
	}

	rhoseConfigOptions = []*sensu.PluginConfigOption{
		{
			Path:      webHookURL,
			Env:       "RHOSE_WEBHOOK_URL",
			Argument:  webHookURL,
			Shorthand: "w",
			Secret:    true,
			Usage:     "The webhook url to send messages to",
			Value:     &config.rhoseURL,
		},
		{
			Path:      clientId,
			Env:       "RHOSE_CLIENT_ID",
			Argument:  clientId,
			Shorthand: "c",
			Secret:    true,
			Usage:     "The client id",
			Value:     &config.clientId,
		},
		{
			Path:      clientSecret,
			Env:       "RHOSE_CLIENT_SECRET",
			Argument:  clientSecret,
			Shorthand: "s",
			Secret:    true,
			Usage:     "The client secret",
			Value:     &config.clientSecret,
		},
	}
)

func main() {
	goHandler := sensu.NewGoHandler(&config.PluginConfig, rhoseConfigOptions, checkArgs, sendMessage)
	goHandler.Execute()
}

func checkArgs(_ *corev2.Event) error {
	// Support deprecated environment variables
	if webhook := os.Getenv("RHOSE_WEBHOOK_URL"); webhook != "" {
		config.rhoseURL = webhook
	}
	if clientId := os.Getenv("RHOSE_CLIENT_ID"); clientId != "" {
		config.clientId = clientId
	}
	if clientSecret := os.Getenv("RHOSE_CLIENT_SECRET"); clientSecret != "" {
		config.clientSecret = clientSecret
	}

	if len(config.rhoseURL) == 0 {
		return fmt.Errorf("--%s or RHOSE_WEBHOOK_URL environment variable is required", webHookURL)
	}
	if len(config.clientId) == 0 {
		return fmt.Errorf("--%s or RHOSE_CLIENT_ID environment variable is required", clientId)
	}
	if len(config.clientSecret) == 0 {
		return fmt.Errorf("--%s or RHOSE_CLIENT_SECRET environment variable is required", clientSecret)
	}
	return nil
}

func sendMessage(event *corev2.Event) error {
	// TODO: retrieve jwt and set it in the request

	ce := cloudevents.NewEvent()
	ce.SetSource("sensu/sensu-rhose-handler")
	ce.SetType("example.type")
	ce.SetData(cloudevents.ApplicationJSON, event)

	a, _ := json.Marshal(&ce)
	fmt.Printf("Event payload %s\n", string(a))
	_, err := http.Post(config.rhoseURL, "json", bytes.NewBuffer(a))

	if err != nil {
		return fmt.Errorf("Failed to send message to RHOSE: %v", err)
	}

	// FUTURE: send to AH
	fmt.Printf("Event sent to RHOSE ingress %s\n", config.rhoseURL)

	return nil
}
