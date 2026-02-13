// internal/auth/auth.go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------

var (
	ErrInvalidSignature     = errors.New("invalid signature")
	ErrNonceExpired         = errors.New("nonce expired or not found")
	ErrInvalidToken         = errors.New("invalid token")
	ErrTokenExpired         = errors.New("token expired")
	ErrTokenMalformed       = errors.New("token malformed")
	ErrInvalidSigningMethod = errors.New("invalid signing method")
	ErrUserNotFound         = errors.New("user not found")
	ErrWalletRequired       = errors.New("wallet address required")
)

// ---------------------------------------------------------------------
// Store interface (for nonce and blacklist)
// ---------------------------------------------------------------------

type Store interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

// ---------------------------------------------------------------------
// JWT Claims
// ---------------------------------------------------------------------

type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	Wallet string    `json:"wallet"`
	jwt.RegisteredClaims
}

// ---------------------------------------------------------------------
// Token Pair
// ---------------------------------------------------------------------

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// ---------------------------------------------------------------------
// WalletAuth – main authentication service
// ---------------------------------------------------------------------

type WalletAuth struct {
	jwtSecret         []byte
	store             Store
	accessExpiration  time.Duration
	refreshExpiration time.Duration
	issuer            string
}

type WalletAuthOptions struct {
	JWTSecret         string
	Store             Store
	AccessExpiration  time.Duration
	RefreshExpiration time.Duration
	Issuer            string
}

func NewWalletAuth(opts WalletAuthOptions) *WalletAuth {
	if opts.AccessExpiration == 0 {
		opts.AccessExpiration = 15 * time.Minute
	}
	if opts.RefreshExpiration == 0 {
		opts.RefreshExpiration = 7 * 24 * time.Hour
	}
	if opts.Issuer == "" {
		opts.Issuer = "canglanfu"
	}
	return &WalletAuth{
		jwtSecret:         []byte(opts.JWTSecret),
		store:             opts.Store,
		accessExpiration:  opts.AccessExpiration,
		refreshExpiration: opts.RefreshExpiration,
		issuer:            opts.Issuer,
	}
}

// ---------------------------------------------------------------------
// Nonce Management
// ---------------------------------------------------------------------

// GenerateNonce creates a random 32-byte hex string and stores it with TTL.
func (a *WalletAuth) GenerateNonce(ctx context.Context, wallet string) (string, error) {
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	key := fmt.Sprintf("nonce:%s", strings.ToLower(wallet))
	if err := a.store.Set(ctx, key, nonce, 5*time.Minute); err != nil {
		return "", fmt.Errorf("failed to store nonce: %w", err)
	}
	return nonce, nil
}

// VerifySignature checks that the signature was signed by the wallet's private key.
func (a *WalletAuth) VerifySignature(wallet, signature, nonce string) error {
	// Prepare Ethereum signed message hash
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(nonce), nonce)
	hash := crypto.Keccak256Hash([]byte(msg))

	// Decode signature
	sig := common.FromHex(signature)
	if len(sig) != 65 {
		return ErrInvalidSignature
	}

	// Adjust recovery ID (EIP-155)
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	// Recover public key
	pubKey, err := crypto.SigToPub(hash.Bytes(), sig)
	if err != nil {
		return fmt.Errorf("failed to recover public key: %w", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	if !strings.EqualFold(recoveredAddr.Hex(), wallet) {
		return ErrInvalidSignature
	}

	return nil
}

// ---------------------------------------------------------------------
// JWT Token Management
// ---------------------------------------------------------------------

// GenerateTokenPair creates both access and refresh tokens.
func (a *WalletAuth) GenerateTokenPair(userID uuid.UUID, wallet string) (*TokenPair, error) {
	// Access token
	accessExp := time.Now().Add(a.accessExpiration)
	accessClaims := &Claims{
		UserID: userID,
		Wallet: wallet,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(accessExp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    a.issuer,
			Subject:   userID.String(),
			ID:        uuid.New().String(),
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString(a.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Refresh token (different expiration, same claims)
	refreshExp := time.Now().Add(a.refreshExpiration)
	refreshClaims := &Claims{
		UserID: userID,
		Wallet: wallet,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    a.issuer,
			Subject:   userID.String(),
			ID:        uuid.New().String(),
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(a.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    accessExp,
		TokenType:    "Bearer",
	}, nil
}

// ValidateToken parses and validates a JWT token.
func (a *WalletAuth) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidSigningMethod
		}
		return a.jwtSecret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		if errors.Is(err, jwt.ErrTokenMalformed) {
			return nil, ErrTokenMalformed
		}
		return nil, ErrInvalidToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	// Verify issuer
	if claims.Issuer != a.issuer {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// RefreshToken generates a new token pair using a valid refresh token.
func (a *WalletAuth) RefreshToken(ctx context.Context, refreshTokenString string) (*TokenPair, error) {
	claims, err := a.ValidateToken(refreshTokenString)
	if err != nil {
		return nil, err
	}

	// Optionally check blacklist for refresh token
	if blacklisted, _ := a.IsTokenBlacklisted(ctx, refreshTokenString); blacklisted {
		return nil, ErrInvalidToken
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, ErrInvalidToken
	}

	return a.GenerateTokenPair(userID, claims.Wallet)
}

// ---------------------------------------------------------------------
// Token Blacklisting (for logout)
// ---------------------------------------------------------------------

// BlacklistToken adds a token to the blacklist until its expiration.
func (a *WalletAuth) BlacklistToken(ctx context.Context, tokenString string) error {
	claims, err := a.ValidateToken(tokenString)
	if err != nil {
		return err
	}

	expTime := claims.ExpiresAt.Time
	if expTime.Before(time.Now()) {
		return nil // already expired
	}

	ttl := time.Until(expTime)
	key := fmt.Sprintf("blacklist:%s", claims.ID)
	return a.store.Set(ctx, key, "blacklisted", ttl)
}

// IsTokenBlacklisted checks if a token is blacklisted.
func (a *WalletAuth) IsTokenBlacklisted(ctx context.Context, tokenString string) (bool, error) {
	// Parse token without validation to get JTI
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := &Claims{}
	_, _, err := parser.ParseUnverified(tokenString, claims)
	if err != nil {
		return false, err
	}

	key := fmt.Sprintf("blacklist:%s", claims.ID)
	val, err := a.store.Get(ctx, key)
	if err != nil {
		return false, nil // treat error as not blacklisted
	}
	return val == "blacklisted", nil
}

// ---------------------------------------------------------------------
// Context Helpers
// ---------------------------------------------------------------------

type contextKey string

const (
	UserIDKey contextKey = "user_id"
	WalletKey contextKey = "wallet"
)

// AuthMiddleware is a Chi middleware that validates the JWT token.
func (a *WalletAuth) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		tokenString := parts[1]

		// Check blacklist
		if blacklisted, _ := a.IsTokenBlacklisted(r.Context(), tokenString); blacklisted {
			http.Error(w, "Token revoked", http.StatusUnauthorized)
			return
		}

		claims, err := a.ValidateToken(tokenString)
		if err != nil {
			if errors.Is(err, ErrTokenExpired) {
				http.Error(w, "Token expired", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
		ctx = context.WithValue(ctx, WalletKey, claims.Wallet)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserID retrieves the authenticated user ID from context.
func GetUserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(UserIDKey).(uuid.UUID)
	return id, ok
}

// GetWallet retrieves the authenticated wallet address from context.
func GetWallet(ctx context.Context) (string, bool) {
	w, ok := ctx.Value(WalletKey).(string)
	return w, ok
}

// ---------------------------------------------------------------------
// In-Memory Store (for development/testing)
// ---------------------------------------------------------------------

type MemoryStore struct {
	data map[string]string
	mu   sync.RWMutex
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]string)}
}

func (m *MemoryStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = fmt.Sprintf("%v", value)
	// In a real implementation you would handle TTL; for simplicity we ignore.
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	if !ok {
		return "", nil
	}
	return val, nil
}

func (m *MemoryStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}
