package driveapi

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "DriveError retryable",
			err:  &DriveError{Code: 429, Retryable: true},
			want: true,
		},
		{
			name: "DriveError not retryable",
			err:  &DriveError{Code: 400, Retryable: false},
			want: false,
		},
		{
			name: "googleapi 429",
			err:  &googleapi.Error{Code: 429},
			want: true,
		},
		{
			name: "googleapi 500",
			err:  &googleapi.Error{Code: 500},
			want: true,
		},
		{
			name: "googleapi 503",
			err:  &googleapi.Error{Code: 503},
			want: true,
		},
		{
			name: "googleapi 400",
			err:  &googleapi.Error{Code: 400},
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDriveError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantCode      int
		wantRetryable bool
	}{
		{
			name:          "nil error",
			err:           nil,
			wantCode:      0,
			wantRetryable: false,
		},
		{
			name:          "DriveError passthrough",
			err:           &DriveError{Code: 429, Message: "rate limit", Retryable: true},
			wantCode:      429,
			wantRetryable: true,
		},
		{
			name:          "googleapi 429",
			err:           &googleapi.Error{Code: 429, Message: "rate limit"},
			wantCode:      429,
			wantRetryable: true,
		},
		{
			name:          "googleapi 500",
			err:           &googleapi.Error{Code: 500, Message: "server error"},
			wantCode:      500,
			wantRetryable: true,
		},
		{
			name:          "googleapi 400",
			err:           &googleapi.Error{Code: 400, Message: "bad request"},
			wantCode:      400,
			wantRetryable: false,
		},
		{
			name:          "generic error",
			err:           errors.New("generic error"),
			wantCode:      0,
			wantRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDriveError(tt.err)
			if tt.err == nil {
				if got != nil {
					t.Errorf("ParseDriveError() = %v, want nil", got)
				}
				return
			}

			if got.Code != tt.wantCode {
				t.Errorf("ParseDriveError().Code = %v, want %v", got.Code, tt.wantCode)
			}
			if got.Retryable != tt.wantRetryable {
				t.Errorf("ParseDriveError().Retryable = %v, want %v", got.Retryable, tt.wantRetryable)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{
			name:  "seconds",
			value: "60",
			want:  60 * time.Second,
		},
		{
			name:  "zero seconds",
			value: "0",
			want:  0,
		},
		{
			name:  "invalid format",
			value: "invalid",
			want:  0,
		},
		{
			name:  "empty string",
			value: "",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.value); got != tt.want {
				t.Errorf("parseRetryAfter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		name      string
		attempt   int
		baseDelay time.Duration
		maxDelay  time.Duration
		want      time.Duration
	}{
		{
			name:      "first attempt",
			attempt:   0,
			baseDelay: 1 * time.Second,
			maxDelay:  32 * time.Second,
			want:      1 * time.Second,
		},
		{
			name:      "second attempt",
			attempt:   1,
			baseDelay: 1 * time.Second,
			maxDelay:  32 * time.Second,
			want:      2 * time.Second,
		},
		{
			name:      "third attempt",
			attempt:   2,
			baseDelay: 1 * time.Second,
			maxDelay:  32 * time.Second,
			want:      4 * time.Second,
		},
		{
			name:      "max delay reached",
			attempt:   10,
			baseDelay: 1 * time.Second,
			maxDelay:  32 * time.Second,
			want:      32 * time.Second,
		},
		{
			name:      "negative attempt",
			attempt:   -1,
			baseDelay: 1 * time.Second,
			maxDelay:  32 * time.Second,
			want:      1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExponentialBackoff(tt.attempt, tt.baseDelay, tt.maxDelay); got != tt.want {
				t.Errorf("ExponentialBackoff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	if cfg.MaxAttempts != 5 {
		t.Errorf("DefaultRetryConfig().MaxAttempts = %v, want 5", cfg.MaxAttempts)
	}
	if cfg.BaseDelay != 1*time.Second {
		t.Errorf("DefaultRetryConfig().BaseDelay = %v, want 1s", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 32*time.Second {
		t.Errorf("DefaultRetryConfig().MaxDelay = %v, want 32s", cfg.MaxDelay)
	}
}

func TestDriveErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *DriveError
		want string
	}{
		{
			name: "without retry after",
			err:  &DriveError{Code: 429, Message: "rate limit"},
			want: "drive API error (code 429): rate limit",
		},
		{
			name: "with retry after",
			err:  &DriveError{Code: 429, Message: "rate limit", RetryAfter: 60 * time.Second},
			want: "drive API error (code 429): rate limit (retry after 1m0s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("DriveError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}
