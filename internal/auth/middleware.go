// internal/auth/middleware.go
package auth

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------
// Authentication Middleware (JWT validation)
// ---------------------------------------------------------------------

// AuthMiddleware is implemented in auth.go (canonical implementation).
// The helpers in this file provide optional/auxiliary middlewares.

// ---------------------------------------------------------------------
// Optional Authentication Middleware
// ---------------------------------------------------------------------

// OptionalAuthMiddleware attempts to authenticate the request but does not
// fail if no token is provided or if the token is invalid.
// It sets the user ID and wallet in the context only if authentication succeeds.
func (a *WalletAuth) OptionalAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
				tokenString := parts[1]
				claims, err := a.ValidateToken(tokenString)
				if err == nil {
					// Token is valid – set context
					ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
					ctx = context.WithValue(ctx, WalletKey, claims.Wallet)
					r = r.WithContext(ctx)
				}
				// On error, simply proceed without authentication
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------
// Role & Node Level Middleware
// ---------------------------------------------------------------------

// RequireNodeLevel returns a middleware that restricts access to users
// with a minimum node level. It must be used after AuthMiddleware.
func (a *WalletAuth) RequireNodeLevel(minLevel int, getUserNodeLevel func(ctx context.Context, userID uuid.UUID) (int, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := GetUserID(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			level, err := getUserNodeLevel(r.Context(), userID)
			if err != nil || level < minLevel {
				http.Error(w, "Insufficient node level", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission checks if the authenticated user has a specific permission.
// Permissions can be derived from node levels, badges, or custom claims.
func (a *WalletAuth) RequirePermission(permission string, hasPermission func(ctx context.Context, userID uuid.UUID, permission string) (bool, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := GetUserID(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			ok, err := hasPermission(r.Context(), userID, permission)
			if err != nil || !ok {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------
// Rate Limiting Middleware (Redis-based)
// ---------------------------------------------------------------------

// RateLimiter limits requests per IP address or per user.
// It requires a Redis client and returns a middleware.
func RateLimiter(redisClient *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try to get user ID from context (if authenticated)
			userID, ok := GetUserID(r.Context())
			var key string
			if ok {
				key = "rate:user:" + userID.String()
			} else {
				// Fallback to IP address
				ip := r.RemoteAddr
				if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
					ip = strings.Split(forwarded, ",")[0]
				}
				key = "rate:ip:" + ip
			}

			ctx := r.Context()
			pipe := redisClient.Pipeline()
			incr := pipe.Incr(ctx, key)
			pipe.Expire(ctx, key, window)
			_, err := pipe.Exec(ctx)
			if err != nil {
				// If Redis fails, allow the request (fail open)
				next.ServeHTTP(w, r)
				return
			}

			count, err := incr.Result()
			if err == nil && count > int64(limit) {
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------
// Two-Factor Authentication Middleware (placeholder)
// ---------------------------------------------------------------------

// Require2FA checks if the user has 2FA enabled and validates the provided
// 2FA code. This is a placeholder – actual implementation would verify TOTP.
func (a *WalletAuth) Require2FA(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetUserID(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// In a real implementation, you would:
		// 1. Check if user has 2FA enabled
		// 2. Extract the 2FA code from header (e.g., X-2FA-Code)
		// 3. Verify the code against the user's secret
		// 4. Reject if invalid or missing

		// For now, we simply pass through (no 2FA enforcement)
		// This should be replaced with actual logic.
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------
// Request ID Propagation
// ---------------------------------------------------------------------

// RequestIDMiddleware ensures a request ID is present in the context.
// This is already provided by chi/middleware, but we include it here
// for completeness and to show how to add custom request ID behavior.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
