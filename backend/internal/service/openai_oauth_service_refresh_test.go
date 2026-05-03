package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type openaiOAuthClientRefreshStub struct {
	refreshCalls int32
}

func (s *openaiOAuthClientRefreshStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientRefreshStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	atomic.AddInt32(&s.refreshCalls, 1)
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientRefreshStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	atomic.AddInt32(&s.refreshCalls, 1)
	return nil, errors.New("not implemented")
}

type openaiOAuthClientRefreshSuccessStub struct{}

func (s *openaiOAuthClientRefreshSuccessStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientRefreshSuccessStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, "")
}

func (s *openaiOAuthClientRefreshSuccessStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	return &openai.TokenResponse{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		ExpiresIn:    3600,
	}, nil
}

func TestOpenAIOAuthService_RefreshAccountToken_NoRefreshTokenUsesExistingAccessToken(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{}
	svc := NewOpenAIOAuthService(nil, client)

	expiresAt := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "existing-access-token",
			"expires_at":   expiresAt,
			"client_id":    "client-id-1",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "existing-access-token", info.AccessToken)
	require.Equal(t, "client-id-1", info.ClientID)
	require.Zero(t, atomic.LoadInt32(&client.refreshCalls), "existing access token should be reused without calling refresh")
}

func TestProvideOpenAIOAuthService_RefreshAccountToken_EnrichesSubscriptionExpiry(t *testing.T) {
	accountCheckServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/accounts/check/v4-2023-04-27":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"accounts": {
					"acct_1": {
						"account": {"plan_type": "plus", "is_default": true},
						"entitlement": {"expires_at": "2026-06-01T00:00:00Z"}
					}
				}
			}`))
		case "/backend-api/settings/account_user_setting":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer accountCheckServer.Close()

	originalAccountsCheckURL := chatGPTAccountsCheckURL
	originalSettingsURL := openAISettingsURL
	chatGPTAccountsCheckURL = accountCheckServer.URL + "/backend-api/accounts/check/v4-2023-04-27"
	openAISettingsURL = accountCheckServer.URL + "/backend-api/settings/account_user_setting"
	defer func() {
		chatGPTAccountsCheckURL = originalAccountsCheckURL
		openAISettingsURL = originalSettingsURL
	}()

	factory := func(proxyURL string) (*req.Client, error) {
		return req.C(), nil
	}
	svc := ProvideOpenAIOAuthService(nil, &openaiOAuthClientRefreshSuccessStub{}, factory)

	info, err := svc.RefreshAccountToken(context.Background(), &Account{
		ID:       88,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "refresh-token",
			"client_id":     "client-id-1",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "plus", info.PlanType)
	require.Equal(t, "2026-06-01T00:00:00Z", info.SubscriptionExpiresAt)

	creds := svc.BuildAccountCredentials(info)
	require.Equal(t, "2026-06-01T00:00:00Z", creds["subscription_expires_at"])
}
