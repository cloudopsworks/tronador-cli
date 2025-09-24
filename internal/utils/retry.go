// internal/utils/retry.go
package utils

import (
	"context"
	"fmt"
	"time"
)

// RetryConfig holds configuration for retry behavior
type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// DefaultRetryConfig returns a sensible default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
}

// RetryWithBackoff executes a function with exponential backoff retry logic
func RetryWithBackoff(ctx context.Context, config RetryConfig, operation func() error) error {
	var lastErr error
	delay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		if attempt > 1 {
			// Check if context is cancelled before sleeping
			select {
			case <-ctx.Done():
				return fmt.Errorf("operation cancelled: %w", ctx.Err())
			case <-time.After(delay):
				// Continue with retry
			}
		}

		err := operation()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// If this was the last attempt, don't calculate next delay
		if attempt == config.MaxAttempts {
			break
		}

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * config.Multiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return fmt.Errorf("operation failed after %d attempts, last error: %w", config.MaxAttempts, lastErr)
}

// SimpleRetry is a convenience function for simple retry scenarios
func SimpleRetry(ctx context.Context, maxAttempts int, operation func() error) error {
	config := RetryConfig{
		MaxAttempts:  maxAttempts,
		InitialDelay: 1 * time.Second,
		MaxDelay:     16 * time.Second,
		Multiplier:   2.0,
	}
	return RetryWithBackoff(ctx, config, operation)
}
