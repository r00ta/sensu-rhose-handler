package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	corev2 "github.com/sensu/sensu-go/api/core/v2"

	"github.com/sensu-community/sensu-plugin-sdk/sensu"
)

// AccessTokenResponse contains the Authorization response object from keycloak
type AccessTokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresAt        int    `json:"expires_at"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	NotBeforePolicy  int    `json:"not-before-policy"`
}

// HandlerConfig contains the Slack handler configuration
type HandlerConfig struct {
	sensu.PluginConfig
	rhoseURL              string
	clientID              string
	clientSecret          string
	ssoURL                string
	authenticationEnabled string
}

const (
	webHookURL            = "webhook-url"
	clientID              = "client-id"
	clientSecret          = "client-secret"
	ssoURL                = "sso-url"
	authenticationEnabled = "authentication-enabled"

	defaultAuthenticationEnabled = "no"
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
			Path:      clientID,
			Env:       "RHOSE_CLIENT_ID",
			Argument:  clientID,
			Shorthand: "c",
			Secret:    true,
			Usage:     "The client id",
			Value:     &config.clientID,
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
		{
			Path:      ssoURL,
			Env:       "SSO_URL",
			Argument:  ssoURL,
			Shorthand: "o",
			Secret:    true,
			Usage:     "The sso to use to retrieve the token",
			Value:     &config.ssoURL,
		},
		{
			Path:      authenticationEnabled,
			Env:       "AUTHENTICATION_ENABLED",
			Argument:  authenticationEnabled,
			Default:   defaultAuthenticationEnabled,
			Shorthand: "a",
			Secret:    true,
			Usage:     "Is the authentication enabled",
			Value:     &config.authenticationEnabled,
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

	if authenticationEnabled := os.Getenv("AUTHENTICATION_ENABLED"); authenticationEnabled != "" && config.authenticationEnabled == defaultAuthenticationEnabled {
		config.authenticationEnabled = authenticationEnabled

		if clientID := os.Getenv("RHOSE_CLIENT_ID"); clientID != "" {
			config.clientID = clientID
		}
		if clientSecret := os.Getenv("RHOSE_CLIENT_SECRET"); clientSecret != "" {
			config.clientSecret = clientSecret
		}
		if ssoURL := os.Getenv("SSO_URL"); ssoURL != "" {
			config.ssoURL = ssoURL
		}

		if len(config.clientID) == 0 {
			return fmt.Errorf("--%s or RHOSE_CLIENT_ID environment variable is required", clientID)
		}
		if len(config.clientSecret) == 0 {
			return fmt.Errorf("--%s or RHOSE_CLIENT_SECRET environment variable is required", clientSecret)
		}
		if len(config.ssoURL) == 0 {
			return fmt.Errorf("--%s or SSO_URL environment variable is required", clientSecret)
		}
	}

	if len(config.rhoseURL) == 0 {
		return fmt.Errorf("--%s or RHOSE_WEBHOOK_URL environment variable is required", webHookURL)
	}

	return nil
}

func sendMessage(event *corev2.Event) error {
	// TODO: retrieve jwt and set it in the request
	client := &http.Client{}

	token := ""
	if config.authenticationEnabled == "yes" {
		data := url.Values{}
		data.Set("grant_type", "client_credentials")
		data.Set("client_id", config.clientID)
		data.Set("client_secret", config.clientSecret)
		req, _ := http.NewRequest("POST", config.ssoURL, strings.NewReader(data.Encode()))

		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("Failed to send message to RHOSE: %v", err)
		}
		defer res.Body.Close()

		var accessTokenResponse AccessTokenResponse
		err = json.NewDecoder(res.Body).Decode(&accessTokenResponse)
		if err != nil {
			return fmt.Errorf("Failed to retrieve jwt token: %v", err)
		}
		token = accessTokenResponse.AccessToken
	}

	ce := cloudevents.NewEvent()
	ce.SetSource("sensu/sensu-rhose-handler")
	ce.SetType("example.type")
	ce.SetData(cloudevents.ApplicationJSON, event)

	a, _ := json.Marshal(&ce)

	fmt.Printf("Event payload %s\n", string(a))

	req, _ := http.NewRequest("POST", config.rhoseURL, bytes.NewBuffer(a))
	req.Header.Add("Content-Type", "application/json")
	if config.authenticationEnabled == "yes" {
		req.Header.Add("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)

	fmt.Printf("Event sent to RHOSE ingress with status code %s\n", http.StatusText(res.StatusCode))

	if err != nil {
		return fmt.Errorf("Failed to send message to RHOSE: %v", err)
	}

	// FUTURE: send to AH
	fmt.Printf("Event sent to RHOSE ingress %s\n", config.rhoseURL)

	return nil
}
