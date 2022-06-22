package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/golang-jwt/jwt/v4"

	corev2 "github.com/sensu/sensu-go/api/core/v2"

	"github.com/MicahParks/keyfunc"
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

	if cachedToken != nil {
		var jwksJSON = json.RawMessage(`{"keys":[{"kid":"-4elc_VdN_WsOUYf2G4Qxr8GcwIx_KtXUCitatLKlLw","kty":"RSA","alg":"RS256","use":"sig","n":"5MvhbE1Mxr2FUYGZiH0z6p-kV-FIUHp4ErxkD6S8Sc5OB7IjRKDSsJzmuwR803cKpeKoIkkUTiznYwCBqAUdP3bIZ8k97X6GX19dOSqL4ej1rjYZYAf9_Jt_Z-0PzIjX50z6TpqeGoh7-6P-634SvbdjatnhTAQ3qsBXfPOHPIPRAZkGfmlM1EdvIlm_d2hQ7nDSETbVC4YHY-iESvUhre-aNmqJU_E6fRnGwFTPS20fPLE5bUNbshvTXn5c-bxtWK9bSCHCRVYUF9QWwDoFX9gGOIpSScHAKQLRR16yOQjOioZ2FeVZnDpWNvZelbQ7LtLN0H5uCJsqDoZDDhDWeFp-25O9ih5M9auT_2IepUlOq3OBMj7i3CJXrvjNQiuGkPHp9xN6kd5H4E5hcqUTmfYdgf1IuXP0cTwYtQor21dWBSpFvxW8l1HGLOaO_rSetNRJ-tZ7FKUK5L6crt1N72AGIay96gNOWNe4POOG_ML1r4h3SKBFdMPwJ-R5KDg7-oRcUT4kLuFtWuQG7bKLJhIxw_SnVFajLGt1d3-OCqX6ozuUbdEW31f9iLZd4w-NUSSHjxP1Uvalk5QfUro9w9fTW73jRIUASnbHunopjt_IkiQswrdIwpfpeBokcf9O757_i0kctQ5M1gyPf4-0yPfuDVkeBAHygoxNJU9H3C0","e":"AQAB","x5c":["MIIE3TCCA8WgAwIBAgIED/4ATjANBgkqhkiG9w0BAQsFADBBMRAwDgYDVQQKDAdSZWQgSGF0MQ0wCwYDVQQLDARwcm9kMR4wHAYDVQQDDBVDZXJ0aWZpY2F0ZSBBdXRob3JpdHkwHhcNMTkwNjA2MjAyODQzWhcNMjQwNjA0MjAyODQzWjCBjDELMAkGA1UEBhMCVVMxFzAVBgNVBAgMDk5vcnRoIENhcm9saW5hMRYwFAYDVQQKDA1SZWQgSGF0LCBJbmMuMQswCQYDVQQLDAJJUzEYMBYGA1UEAwwPcHJvZC1wdWJsaWMtaWRwMSUwIwYJKoZIhvcNAQkBFhZzZXJ2aWNlZGVza0ByZWRoYXQuY29tMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA5MvhbE1Mxr2FUYGZiH0z6p+kV+FIUHp4ErxkD6S8Sc5OB7IjRKDSsJzmuwR803cKpeKoIkkUTiznYwCBqAUdP3bIZ8k97X6GX19dOSqL4ej1rjYZYAf9/Jt/Z+0PzIjX50z6TpqeGoh7+6P+634SvbdjatnhTAQ3qsBXfPOHPIPRAZkGfmlM1EdvIlm/d2hQ7nDSETbVC4YHY+iESvUhre+aNmqJU/E6fRnGwFTPS20fPLE5bUNbshvTXn5c+bxtWK9bSCHCRVYUF9QWwDoFX9gGOIpSScHAKQLRR16yOQjOioZ2FeVZnDpWNvZelbQ7LtLN0H5uCJsqDoZDDhDWeFp+25O9ih5M9auT/2IepUlOq3OBMj7i3CJXrvjNQiuGkPHp9xN6kd5H4E5hcqUTmfYdgf1IuXP0cTwYtQor21dWBSpFvxW8l1HGLOaO/rSetNRJ+tZ7FKUK5L6crt1N72AGIay96gNOWNe4POOG/ML1r4h3SKBFdMPwJ+R5KDg7+oRcUT4kLuFtWuQG7bKLJhIxw/SnVFajLGt1d3+OCqX6ozuUbdEW31f9iLZd4w+NUSSHjxP1Uvalk5QfUro9w9fTW73jRIUASnbHunopjt/IkiQswrdIwpfpeBokcf9O757/i0kctQ5M1gyPf4+0yPfuDVkeBAHygoxNJU9H3C0CAwEAAaOBkDCBjTAfBgNVHSMEGDAWgBR72gn1SV3Z11zJNvhV0huXnhEvfjA7BggrBgEFBQcBAQQvMC0wKwYIKwYBBQUHMAGGH2h0dHA6Ly9vY3NwLnJlZGhhdC5jb20vY2Evb2NzcC8wDgYDVR0PAQH/BAQDAgTwMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjANBgkqhkiG9w0BAQsFAAOCAQEAe5yOuIECoiofNfhz66AwiGhiDHb0EAmjHZWX5AmKSZMV7n0nl4XZkIAxFudOPIFRXJvArTjC2SMgU0NnVVuxMJRUl+mECAHy7olDcBF76coA4xRQdp886ZAzWw0aRq/bs6Xpjgl7YZRD2lSXItPuuTzSfH2Cqij+GOchsRwIN3g4KiTwpMJ9070vHdCn3uB1OozrJiG6L698vlEL8nofgi58rNKSCG7v8wX7TSx5CBTkkGCr95/6NzhHUni9CWG/k/ef0DMwXdX4CSO7lPEa+QWoHTgTgvEZuZGrdP1O9+cezlPjmvXguwywEXHKAy4bk3PYUXjniQVBdEa6gUUglA=="],"x5t":"oMp1WOqwffC24QIji-YsqlBH4ho","x5t#S256":"5HxnIVL1KGgpyvaJ2GY6lracomnPb84vcji7aaJo-DU"},{"kid":"v5MpUEnwk1VYIqifv9G9xmIB2ZLzPttk-0PaEURQQ3I","kty":"RSA","alg":"RS256","use":"sig","n":"uYp35gi5YzQeNN5aQOPwLranSJT9aJB-w6Ih4Wn9R6FzEg1OEKwBNNpb-z18reAyhxQMy_bCz3q-J7viX6p5hbclPBakKOjPB4lDzwhvfE1G4vp84zH1bR7m8dd4OXbriojVZ51IPNuItO00nrDrx6PWNP_5ufBUwjJo8-BD-sWm7BP_CVlb8miVh8itpcLJrszpHzF-u0OPqwI_e3P83cYOsXoQRxD4wpo718yqYh4J3NNJQYnyprJMpC3w3QQ5PR28TbBfSHgvtWD1SBuavHh2jwT_6Pi8FqOS1vfX7QA1pxyYZ-zazVxj_zOrCeP3FHyaxTPmn0d5zsXBZCCyhsfCaStnFePTPk-KEGwZAlv43JJjV2rTJc1Lsj1Th7Jq63TvwIGBcFFAtC72N5-jwRjUoeyu_nwO_1r1awvbfrlBF31PG5wxUdVR56PesLO7EVH1_2KrVN7dtgaQkomVk6rULBbCbwhfR1oT3cOxF7d0ajpbzHd2qcfeBzFTABL8dzBp4FcZx5QyYSIOP8fuwSO8zy4rxmBw7HpHGOGFrC3cXWqB33M23IjOpVZbfK46QvJhcGq9QEtOlRO2WVemMcwDSgpceAa7e3ZJx-LO6XyTEjRtTuHMwdLxII3YUlL1hPozrNE1U_ADPGHgnTxGswgBpGOA6rOkWav5uhcj9Cs","e":"AQAB","x5c":["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"],"x5t":"EFbl4HHewYst5oFGnF5jnzY3Org","x5t#S256":"D8omucfR2PQb4Kobku3HwcXtGyYOThiqeqADirMLlMk"},{"kid":"RIENZmfJ6O4rpkmnswmxgMUznjq3rRuUbz5r9eFiq3E","kty":"RSA","alg":"RS512","use":"sig","n":"0BpyPqFrZHF2xluG8wSjUMr_ouktSJiSq3VcOn6xH04rG8wLX-v3JfhXRjtJl3XpSJU7j5GMJzz3Cq3dbgBCpb49gVQkBE7s4NVlN4gLhonn7VekXF6YZlI152ROFxoKWda157BIj3m--JYVKIiVg21WujAOA5WVjy17t3fC_7HDgPMVO6MSo7aCbzOc1NEDJ0-5NBNtqZBBlu240gyhW8FNgIdgna-_QWKsQOUKTDlvOFwEt0IDXd29KU0FOIGRPiKsQ--1eIBg3OLMxlni-DhWBAyVpf5_kP0P8udSqXfWba6i6YmnNAAdaVYV5_EGYCxPhwdwTndNtDErCw1oaw","e":"AQAB","x5c":["MIICrTCCAZUCBgF3qV5ZcjANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDDA9yZWRoYXQtZXh0ZXJuYWwwHhcNMjEwMjE2MDU0MjQ4WhcNMzEwMjE2MDU0NDI4WjAaMRgwFgYDVQQDDA9yZWRoYXQtZXh0ZXJuYWwwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDQGnI+oWtkcXbGW4bzBKNQyv+i6S1ImJKrdVw6frEfTisbzAtf6/cl+FdGO0mXdelIlTuPkYwnPPcKrd1uAEKlvj2BVCQETuzg1WU3iAuGieftV6RcXphmUjXnZE4XGgpZ1rXnsEiPeb74lhUoiJWDbVa6MA4DlZWPLXu3d8L/scOA8xU7oxKjtoJvM5zU0QMnT7k0E22pkEGW7bjSDKFbwU2Ah2Cdr79BYqxA5QpMOW84XAS3QgNd3b0pTQU4gZE+IqxD77V4gGDc4szGWeL4OFYEDJWl/n+Q/Q/y51Kpd9ZtrqLpiac0AB1pVhXn8QZgLE+HB3BOd020MSsLDWhrAgMBAAEwDQYJKoZIhvcNAQELBQADggEBAMXDKJzYdo180lqIWKSunJNdGTPzKFEULSn9xWbmfWfAS/NtIVns4OkyMTzpBluePpeuAIIox1xYsqW72Po4bANL95UFThT0Ms8CyviEpBqwlZkRvVDzXC5vieKT4TYOlX6Xue0pguoNk0/jE+R6DvVbdypUzTviebyXvVzdHjzrAgQOML31r4AIsxD07e/IwowPqc3asvSwkewmbZelC9b4yyyyeZLx+RS2Z7WO6rIVELBsoYd6jZWEowABXJxeuiKTNN/1oSpyhHCfrxt8daOmUhXXN3aErKG2vkbmVPFLJBDoBLJPcpeXCisbkaoDu5a0xcs3oQimJtVQNpVDs1A="],"x5t":"yitUkoAFk03oya4Xy_9VromEimw","x5t#S256":"lP-UnUki2KXO2vTXBkho7niFGg4aMveCUb7sFaC5tGY"},{"kid":"E3DKGdZQ7xTiIvfdFgVXLNupVupFBlcxNUgVCFhDwEg","kty":"RSA","alg":"RS512","use":"sig","n":"ta1xAjqdqnH_RlDI1rFtiGWYgnxpzqGflSQXzuiKR1QaipHTeGeLDUTcG1O6nlb9YgEVcJKSP8JQ36QNfXCPKlNcsqUqr81jiL_kSNAD3xHX4Z8ymuA-FW24bLeNwRkdGKGy3aY4giJxXnqB63ArtjmmWaGYEQEriUz16wW0w3H_QJyje3__j_Sh1ya_V7Ct3A6ajTipp-OzAuIgsqXbZz2b8ejr3My5PiXz9t41xKx_u4Mm18BQ4SQ2OvTfA0Of0mZ3Q-FVy2q1WIKwPmCMDyV5bigmvRYblRDCbTvKIGHyEjs1zuAxJqzFJkGpAHpnKfbUdSfO-JWK6fB4V3bPzw","e":"AQAB","x5c":["MIICrTCCAZUCBgF3qV4+jzANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDDA9yZWRoYXQtZXh0ZXJuYWwwHhcNMjEwMjE2MDU0MjQxWhcNMzEwMjE2MDU0NDIxWjAaMRgwFgYDVQQDDA9yZWRoYXQtZXh0ZXJuYWwwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQC1rXECOp2qcf9GUMjWsW2IZZiCfGnOoZ+VJBfO6IpHVBqKkdN4Z4sNRNwbU7qeVv1iARVwkpI/wlDfpA19cI8qU1yypSqvzWOIv+RI0APfEdfhnzKa4D4Vbbhst43BGR0YobLdpjiCInFeeoHrcCu2OaZZoZgRASuJTPXrBbTDcf9AnKN7f/+P9KHXJr9XsK3cDpqNOKmn47MC4iCypdtnPZvx6OvczLk+JfP23jXErH+7gybXwFDhJDY69N8DQ5/SZndD4VXLarVYgrA+YIwPJXluKCa9FhuVEMJtO8ogYfISOzXO4DEmrMUmQakAemcp9tR1J874lYrp8HhXds/PAgMBAAEwDQYJKoZIhvcNAQELBQADggEBALWRXIDVRxEux7UleQbyuA8+jdTRzhScTBiL24NHzRofg5jcWjhCyGxitrhp16sC7+lEQaPTcNGmJIk0uVtExGm6N1WG653Ubkq+KaiQiJPFELZS31x7xLAUo7aNHPVbS6Rr4ufUiFcT2cS0e7sjVlf9FvtX9fdg1TSpq52Vaayz4RXYCx+IrHEmU0L5qDJPyHiuBJ8VvnkcQMqYZ5aAA1z0/HSsF7AIraeyPbQANfJSuvFIPR0+fk/pcvUMB/XMk3obMXYzUMAa4BcOnVcmymcNc8Tf5kwqDIy6Y03yIVRrvKX5aPsBRqAzUtNE4rLkPqhBV+U0dR/xFiLDn3cGyjk="],"x5t":"ZHBbdjfzncqH7ewCO4h6h0HKCUM","x5t#S256":"j0wxZVV5frSC2rs_Kg6cK8RSwDKXMUMwSPPqd3XCO6c"}]}`)
		// Create the JWKS from the resource at the given URL.
		jwks, err := keyfunc.NewJSON(jwksJSON)
		if err != nil {
			fmt.Println("fail!")
			log.Fatalf("Failed to create JWKS from JSON.\nError:%s", err.Error())
		}

		decodedToken, err := jwt.Parse(cachedToken.AccessToken, jwks.Keyfunc)
		if err != nil {
			fmt.Println("Error decoding the token", err)
		} else {
			if decodedToken.Valid {
				used = "yes"
				return cachedToken.AccessToken, nil
			}
		}
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
