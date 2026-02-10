// Package driveapi provides a client wrapper for Google Drive API v3.
package driveapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/api/googleapi"
)

// DriveError represents an error from the Google Drive API.
type DriveError struct {
	Code       int           // HTTP status code
	Message    string        // Error message
	Retryable  bool          // Whether the error is retryable
	RetryAfter time.Duration // Duration to wait before retrying (from Retry-After header)
}

// Error implements the error interface.
func (e *DriveError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("drive API error (code %d): %s (retry after %v)", e.Code, e.Message, e.RetryAfter)
	}
	return fmt.Sprintf("drive API error (code %d): %s", e.Code, e.Message)
}

// IsRetryable checks if an error warrants a retry.
// Retryable errors include:
// - 429 (Rate Limit Exceeded)
// - 500 (Internal Server Error)
// - 502 (Bad Gateway)
// - 503 (Service Unavailable)
// - 504 (Gateway Timeout)
// - Network timeouts and temporary errors
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's our DriveError type
	var driveErr *DriveError
	if errors.As(err, &driveErr) {
		return driveErr.Retryable
	}

	// Check for Google API error
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return isRetryableStatusCode(apiErr.Code)
	}

	// Check for network timeout or temporary errors
	// TODO: Add more sophisticated network error detection if needed
	return false
}

// isRetryableStatusCode returns true if the HTTP status code indicates a retryable error.
func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}

// ParseDriveError converts a Google API error to a DriveError.
func ParseDriveError(err error) *DriveError {
	if err == nil {
		return nil
	}

	// Check if it's already a DriveError
	var driveErr *DriveError
	if errors.As(err, &driveErr) {
		return driveErr
	}

	// Try to parse Google API error
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		de := &DriveError{
			Code:      apiErr.Code,
			Message:   apiErr.Message,
			Retryable: isRetryableStatusCode(apiErr.Code),
		}

		// Parse Retry-After header if present
		if apiErr.Code == http.StatusTooManyRequests || apiErr.Code == http.StatusServiceUnavailable {
			if retryAfter := apiErr.Header.Get("Retry-After"); retryAfter != "" {
				de.RetryAfter = parseRetryAfter(retryAfter)
			}
		}

		return de
	}

	// Generic error
	return &DriveError{
		Code:      0,
		Message:   err.Error(),
		Retryable: false,
	}
}

// parseRetryAfter parses the Retry-After header value.
// It can be either a number of seconds or an HTTP date.
func parseRetryAfter(value string) time.Duration {
	// Try parsing as seconds first
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP date
	if t, err := http.ParseTime(value); err == nil {
		duration := time.Until(t)
		if duration > 0 {
			return duration
		}
	}

	return 0
}

// ExponentialBackoff calculates the wait duration for exponential backoff.
// It uses the formula: min(base * 2^attempt, maxDelay)
func ExponentialBackoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// Calculate exponential backoff: base * 2^attempt
	delay := baseDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay > maxDelay {
			return maxDelay
		}
	}

	return delay
}

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	MaxAttempts int           // Maximum number of retry attempts
	BaseDelay   time.Duration // Base delay for exponential backoff
	MaxDelay    time.Duration // Maximum delay between retries
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Second,
		MaxDelay:    32 * time.Second,
	}
}
