package driveapi

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		token     *oauth2.Token
		accountID string
		wantErr   bool
	}{
		{
			name: "valid token",
			token: &oauth2.Token{
				AccessToken: "test-token",
				Expiry:      time.Now().Add(1 * time.Hour),
			},
			accountID: "test-account",
			wantErr:   false,
		},
		{
			name:      "nil token",
			token:     nil,
			accountID: "test-account",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client, err := NewClient(ctx, zap.NewNop(), tt.accountID, tt.token)

			if tt.wantErr {
				if err == nil {
					t.Error("NewClient() error = nil, want error")
				}
				return
			}

			if err != nil {
				t.Errorf("NewClient() error = %v, want nil", err)
				return
			}

			if client == nil {
				t.Error("NewClient() returned nil client")
				return
			}

			if client.accountID != tt.accountID {
				t.Errorf("NewClient().accountID = %v, want %v", client.accountID, tt.accountID)
			}

			if client.svc == nil {
				t.Error("NewClient().svc is nil")
			}

			if client.rateLimiter == nil {
				t.Error("NewClient().rateLimiter is nil")
			}
		})
	}
}

func TestClientSetRetryConfig(t *testing.T) {
	ctx := context.Background()
	token := &oauth2.Token{
		AccessToken: "test-token",
		Expiry:      time.Now().Add(1 * time.Hour),
	}

	client, err := NewClient(ctx, zap.NewNop(), "test-account", token)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	newConfig := RetryConfig{
		MaxAttempts: 10,
		BaseDelay:   2 * time.Second,
		MaxDelay:    64 * time.Second,
	}

	client.SetRetryConfig(newConfig)

	if client.retryConfig.MaxAttempts != newConfig.MaxAttempts {
		t.Errorf("SetRetryConfig() MaxAttempts = %v, want %v", client.retryConfig.MaxAttempts, newConfig.MaxAttempts)
	}
	if client.retryConfig.BaseDelay != newConfig.BaseDelay {
		t.Errorf("SetRetryConfig() BaseDelay = %v, want %v", client.retryConfig.BaseDelay, newConfig.BaseDelay)
	}
	if client.retryConfig.MaxDelay != newConfig.MaxDelay {
		t.Errorf("SetRetryConfig() MaxDelay = %v, want %v", client.retryConfig.MaxDelay, newConfig.MaxDelay)
	}
}

func TestClientService(t *testing.T) {
	ctx := context.Background()
	token := &oauth2.Token{
		AccessToken: "test-token",
		Expiry:      time.Now().Add(1 * time.Hour),
	}

	client, err := NewClient(ctx, zap.NewNop(), "test-account", token)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	svc := client.Service()
	if svc == nil {
		t.Error("Service() returned nil")
	}
}

func TestClientAccountID(t *testing.T) {
	ctx := context.Background()
	token := &oauth2.Token{
		AccessToken: "test-token",
		Expiry:      time.Now().Add(1 * time.Hour),
	}

	accountID := "test-account-123"
	client, err := NewClient(ctx, zap.NewNop(), accountID, token)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	if got := client.AccountID(); got != accountID {
		t.Errorf("AccountID() = %v, want %v", got, accountID)
	}
}

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10.0, 10)
	if rl == nil {
		t.Fatal("NewRateLimiter() returned nil")
	}

	if rl.limiter == nil {
		t.Error("NewRateLimiter().limiter is nil")
	}

	// Test Allow method
	if !rl.Allow() {
		t.Error("RateLimiter.Allow() = false, want true on first call")
	}
}

func TestUpdateClient(t *testing.T) {
	ctx := context.Background()
	token := &oauth2.Token{
		AccessToken: "test-token",
		Expiry:      time.Now().Add(1 * time.Hour),
	}

	client, err := NewClient(ctx, zap.NewNop(), "test-account", token)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	newToken := &oauth2.Token{
		AccessToken: "new-test-token",
		Expiry:      time.Now().Add(2 * time.Hour),
	}

	err = UpdateClient(ctx, client, newToken)
	if err != nil {
		t.Errorf("UpdateClient() error = %v, want nil", err)
	}

	// Test with nil token
	err = UpdateClient(ctx, client, nil)
	if err == nil {
		t.Error("UpdateClient() with nil token error = nil, want error")
	}
}
