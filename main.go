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

	"github.com/sensu/sensu-plugin-sdk/sensu"
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

var cachedToken *AccessTokenResponse
var used string = "no"

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

func getToken() (string, error) {
	if config.authenticationEnabled != "yes" {
		return "", nil
	}

	if cachedToken != nil { //&& !isJWTTokenExpired(cachedToken.AccessToken) {
		fmt.Println("CACHED")
		return cachedToken.AccessToken, nil
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", config.clientID)
	data.Set("client_secret", config.clientSecret)
	req, _ := http.NewRequest("POST", config.ssoURL, strings.NewReader(data.Encode()))

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Failed to send message to RHOSE: %v", err)
	}
	defer res.Body.Close()

	var accessTokenResponse AccessTokenResponse
	err = json.NewDecoder(res.Body).Decode(&accessTokenResponse)
	if err != nil {
		return "", fmt.Errorf("Failed to retrieve jwt token: %v", err)
	}
	cachedToken = &accessTokenResponse
	return accessTokenResponse.AccessToken, nil
}

func sendMessage(event *corev2.Event) error {
	// TODO: retrieve jwt and set it in the request
	client := &http.Client{}

	token, err := getToken()
	if err != nil {
		return fmt.Errorf("Failed to get token from sso %s", err)
	}

	token, err = getToken()
	if err != nil {
		return fmt.Errorf("Failed to get token from sso %s", err)
	}

	ce := cloudevents.NewEvent()
	ce.SetSource("sensu/sensu-rhose-handler")
	ce.SetType("example.type")
	ce.SetData(cloudevents.ApplicationJSON, event)
	ce.SetExtension("refreshed", used)

	a, _ := json.Marshal(&ce)

	fmt.Printf("Event payload %s\n", string(a))

	req, _ := http.NewRequest("POST", config.rhoseURL, bytes.NewBuffer(a))
	req.Header.Add("Content-Type", "application/cloudevents+json")
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

// This method returns true if JWT token is expired, otherwise returns false
// func isJWTTokenExpired(accessToken string) bool {
// 	var jwksJSON json.RawMessage = []byte(`{"keys":[{"kty":"RSA","e":"AQAB","use":"sig","kid":"MjhhMDk2N2M2NGEwMzgzYjk2OTI3YzdmMGVhOGYxNjI2OTc5Y2Y2MQ","alg":"RS256","n":"zZU9xSgK77PbtkjJgD2Vmmv6_QNe8B54eyOV0k5K2UwuSnhv9RyRA3aL7gDN-qkANemHw3H_4Tc5SKIMltVIYdWlOMW_2m3gDBOODjc1bE-WXEWX6nQkLAOkoFrGW3bgW8TFxfuwgZVTlb6cYkSyiwc5ueFV2xNqo96Qf7nm5E7KZ2QDTkSlNMdW-jIVHMKjuEsy_gtYMaEYrwk5N7VoiYwePaF3I0_g4G2tIrKTLb8DvHApsN1h-s7jMCQFBrY4vCf3RBlYULr4Nz7u8G2NL_L9vURSCU2V2A8rYRkoZoZwk3a3AyJiqeC4T_1rmb8XdrgeFHB5bzXZ7EI0TObhlw"}]}`)

// 	// Create the JWKS from the resource at the given URL.
// 	jwks, err := keyfunc.NewJSON(jwksJSON)
// 	if err != nil {
// 		log.Fatalf("Failed to create JWKS from resource at the given URL.\nError: %s", err.Error())
// 	}

// 	token, tokenErr := jwt.Parse(accessToken, jwks.Keyfunc)
// 	if tokenErr != nil {
// 		fmt.Println(tokenErr)
// 		return true
// 	}
// 	tokenClaims := token.Claims.(jwt.MapClaims)
// 	exp := tokenClaims["exp"].(float64)
// 	return exp-float64(time.Now().Unix()) <= 0
// }
