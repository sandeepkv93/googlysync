package driveapi

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// Client is a wrapper around the Google Drive API service.
type Client struct {
	logger      *zap.Logger
	svc         *drive.Service
	accountID   string
	rateLimiter *rate.Limiter
	retryConfig RetryConfig
}

// NewClient creates a new Drive API client with the given OAuth2 token.
// The client includes rate limiting (10 requests/second) and retry logic.
func NewClient(ctx context.Context, logger *zap.Logger, accountID string, token *oauth2.Token) (*Client, error) {
	if token == nil {
		return nil, fmt.Errorf("token cannot be nil")
	}

	// Create OAuth2 HTTP client
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))

	// Create Drive service
	svc, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create Drive service: %w", err)
	}

	// Create rate limiter: 10 requests per second with burst of 10
	rateLimiter := rate.NewLimiter(rate.Limit(10), 10)

	return &Client{
		logger:      logger.With(zap.String("account_id", accountID)),
		svc:         svc,
		accountID:   accountID,
		rateLimiter: rateLimiter,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// SetRetryConfig updates the retry configuration for this client.
func (c *Client) SetRetryConfig(cfg RetryConfig) {
	c.retryConfig = cfg
}

// executeWithRetry executes an operation with exponential backoff retry logic.
// It respects rate limiting, retries on retryable errors, and honors Retry-After headers.
func (c *Client) executeWithRetry(ctx context.Context, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt <= c.retryConfig.MaxAttempts; attempt++ {
		// Wait for rate limiter
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait: %w", err)
		}

		// Execute operation
		err := operation()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if error is retryable
		if !IsRetryable(err) {
			c.logger.Debug("non-retryable error",
				zap.Error(err),
				zap.Int("attempt", attempt))
			return err
		}

		// Don't retry if we've exhausted attempts
		if attempt >= c.retryConfig.MaxAttempts {
			c.logger.Warn("max retry attempts exhausted",
				zap.Error(err),
				zap.Int("attempts", attempt+1))
			break
		}

		// Calculate wait duration
		driveErr := ParseDriveError(err)
		var waitDuration time.Duration

		if driveErr != nil && driveErr.RetryAfter > 0 {
			// Honor Retry-After header
			waitDuration = driveErr.RetryAfter
			c.logger.Debug("retrying after server-specified delay",
				zap.Duration("wait", waitDuration),
				zap.Int("attempt", attempt))
		} else {
			// Use exponential backoff
			waitDuration = ExponentialBackoff(attempt, c.retryConfig.BaseDelay, c.retryConfig.MaxDelay)
			c.logger.Debug("retrying with exponential backoff",
				zap.Duration("wait", waitDuration),
				zap.Int("attempt", attempt))
		}

		// Wait before retrying
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", c.retryConfig.MaxAttempts+1, lastErr)
}

// Service returns the underlying Drive service.
// This is useful for operations not yet wrapped by this client.
func (c *Client) Service() *drive.Service {
	return c.svc
}

// AccountID returns the account ID associated with this client.
func (c *Client) AccountID() string {
	return c.accountID
}

// RateLimiter is a simple rate limiter using token bucket algorithm.
// This is already provided by golang.org/x/time/rate, but we define
// this type for documentation purposes.
type RateLimiter struct {
	limiter *rate.Limiter
}

// NewRateLimiter creates a new rate limiter with the specified rate and burst.
func NewRateLimiter(requestsPerSecond float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), burst),
	}
}

// Wait blocks until the rate limiter allows the request.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.limiter.Wait(ctx)
}

// Allow checks if a request can be made immediately without waiting.
func (rl *RateLimiter) Allow() bool {
	return rl.limiter.Allow()
}

// UpdateClient updates the OAuth2 token for the client.
// This is useful when the token is refreshed by the auth service.
func UpdateClient(ctx context.Context, client *Client, token *oauth2.Token) error {
	if token == nil {
		return fmt.Errorf("token cannot be nil")
	}

	// Create new HTTP client with updated token
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))

	// Create new Drive service
	svc, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return fmt.Errorf("failed to create Drive service: %w", err)
	}

	// Update the client's service
	client.svc = svc

	return nil
}
