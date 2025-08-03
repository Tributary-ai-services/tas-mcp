// Package federation provides authentication management for MCP servers
package federation

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	// DefaultTokenExpiry is the default expiry time for authentication tokens
	DefaultTokenExpiry = 24 * time.Hour
	// OAuth2ExpiresIn is the OAuth2 token expiry time in seconds
	OAuth2ExpiresIn = 3600 // seconds
	// JWTExpiresIn is the JWT token expiry time in seconds
	JWTExpiresIn = 3600 // seconds
	// BasicAuthExpiry is the Basic auth token expiry time in seconds (24 hours)
	BasicAuthExpiry = 86400 // 24 hours in seconds
	// HTTPTimeout is the timeout for HTTP requests
	HTTPTimeout = 30 * time.Second
	// BasicAuthParts is the expected number of parts in basic auth credentials
	BasicAuthParts = 2
)

// AuthenticationManager manages authentication for MCP servers
type AuthenticationManager struct {
	logger           *zap.Logger
	providers        map[string]AuthProvider
	tokenCache       map[string]*CachedToken
	jwtVerifyingKeys map[string]interface{}
	mu               sync.RWMutex
}

// AuthProvider defines the interface for authentication providers
type AuthProvider interface {
	// Authenticate performs authentication and returns a token
	Authenticate(ctx context.Context, config AuthConfig) (*AuthToken, error)

	// Validate validates an existing token
	Validate(ctx context.Context, token string) (*TokenInfo, error)

	// Refresh refreshes an existing token
	Refresh(ctx context.Context, refreshToken string) (*AuthToken, error)

	// GetType returns the authentication type this provider handles
	GetType() AuthType
}

// AuthToken represents an authentication token
type AuthToken struct {
	AccessToken  string            `json:"access_token"`
	TokenType    string            `json:"token_type"`
	ExpiresIn    int64             `json:"expires_in"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	Scope        string            `json:"scope,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	IssuedAt     time.Time         `json:"issued_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

// TokenInfo represents validated token information
type TokenInfo struct {
	Valid     bool                   `json:"valid"`
	Subject   string                 `json:"subject,omitempty"`
	Issuer    string                 `json:"issuer,omitempty"`
	Audience  []string               `json:"audience,omitempty"`
	ExpiresAt time.Time              `json:"expires_at"`
	IssuedAt  time.Time              `json:"issued_at"`
	Claims    map[string]interface{} `json:"claims,omitempty"`
	Scopes    []string               `json:"scopes,omitempty"`
}

// CachedToken represents a cached authentication token
type CachedToken struct {
	Token     *AuthToken
	ServerID  string
	CachedAt  time.Time
	ExpiresAt time.Time
}

// NewAuthenticationManager creates a new authentication manager
func NewAuthenticationManager(logger *zap.Logger) *AuthenticationManager {
	manager := &AuthenticationManager{
		logger:           logger,
		providers:        make(map[string]AuthProvider),
		tokenCache:       make(map[string]*CachedToken),
		jwtVerifyingKeys: make(map[string]interface{}),
	}

	// Register default providers
	manager.registerDefaultProviders()

	return manager
}

// RegisterProvider registers an authentication provider
func (am *AuthenticationManager) RegisterProvider(provider AuthProvider) error {
	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	authType := string(provider.GetType())
	am.providers[authType] = provider

	am.logger.Info("Registered authentication provider", zap.String("type", authType))
	return nil
}

// GetAuthToken gets an authentication token for a server
func (am *AuthenticationManager) GetAuthToken(ctx context.Context, serverID string, config AuthConfig) (*AuthToken, error) {
	// Check cache first
	if token := am.getCachedToken(serverID); token != nil {
		return token, nil
	}

	// Get provider
	provider, err := am.getProvider(config.Type)
	if err != nil {
		return nil, err
	}

	// Authenticate
	token, err := provider.Authenticate(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Cache token
	am.cacheToken(serverID, token)

	am.logger.Info("Successfully authenticated server",
		zap.String("server_id", serverID),
		zap.String("auth_type", string(config.Type)))

	return token, nil
}

// ValidateToken validates an authentication token
func (am *AuthenticationManager) ValidateToken(ctx context.Context, token string, authType AuthType) (*TokenInfo, error) {
	provider, err := am.getProvider(authType)
	if err != nil {
		return nil, err
	}

	return provider.Validate(ctx, token)
}

// RefreshToken refreshes an authentication token
func (am *AuthenticationManager) RefreshToken(ctx context.Context, serverID, refreshToken string, authType AuthType) (*AuthToken, error) {
	provider, err := am.getProvider(authType)
	if err != nil {
		return nil, err
	}

	token, err := provider.Refresh(ctx, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	// Update cache
	am.cacheToken(serverID, token)

	am.logger.Info("Successfully refreshed token for server", zap.String("server_id", serverID))
	return token, nil
}

// AddBearerAuth adds bearer authentication to an HTTP request
func (am *AuthenticationManager) AddBearerAuth(req *http.Request, serverID string, config AuthConfig) error {
	token, err := am.GetAuthToken(req.Context(), serverID, config)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	return nil
}

// AddAPIKeyAuth adds API key authentication to an HTTP request
func (am *AuthenticationManager) AddAPIKeyAuth(req *http.Request, config AuthConfig) error {
	apiKey, ok := config.Config["key"]
	if !ok {
		return fmt.Errorf("API key not found in config")
	}

	header := config.Config["header"]
	if header == "" {
		header = "X-API-Key"
	}

	req.Header.Set(header, apiKey)
	return nil
}

// AddBasicAuth adds basic authentication to an HTTP request
func (am *AuthenticationManager) AddBasicAuth(req *http.Request, config AuthConfig) error {
	username, ok := config.Config["username"]
	if !ok {
		return fmt.Errorf("username not found in config")
	}

	password, ok := config.Config["password"]
	if !ok {
		return fmt.Errorf("password not found in config")
	}

	req.SetBasicAuth(username, password)
	return nil
}

// AddAuthentication adds appropriate authentication to an HTTP request
func (am *AuthenticationManager) AddAuthentication(req *http.Request, serverID string, config AuthConfig) error {
	switch config.Type {
	case AuthNone:
		return nil
	case AuthAPIKey:
		return am.AddAPIKeyAuth(req, config)
	case AuthBasic:
		return am.AddBasicAuth(req, config)
	case AuthBearer, AuthJWT, AuthOAuth2:
		return am.AddBearerAuth(req, serverID, config)
	default:
		return fmt.Errorf("unsupported authentication type: %s", config.Type)
	}
}

// CleanupExpiredTokens removes expired tokens from cache
func (am *AuthenticationManager) CleanupExpiredTokens() {
	am.mu.Lock()
	defer am.mu.Unlock()

	now := time.Now()
	for key, cached := range am.tokenCache {
		if now.After(cached.ExpiresAt) {
			delete(am.tokenCache, key)
			am.logger.Debug("Removed expired token from cache", zap.String("server_id", cached.ServerID))
		}
	}
}

// GetCacheStats returns statistics about the token cache
func (am *AuthenticationManager) GetCacheStats() map[string]interface{} {
	am.mu.RLock()
	defer am.mu.RUnlock()

	now := time.Now()
	total := len(am.tokenCache)
	expired := 0

	for _, cached := range am.tokenCache {
		if now.After(cached.ExpiresAt) {
			expired++
		}
	}

	return map[string]interface{}{
		"total_tokens":   total,
		"expired_tokens": expired,
		"active_tokens":  total - expired,
	}
}

// registerDefaultProviders registers built-in authentication providers
func (am *AuthenticationManager) registerDefaultProviders() {
	// Register OAuth2 provider
	oauth2Provider := NewOAuth2Provider(am.logger)
	_ = am.RegisterProvider(oauth2Provider)

	// Register JWT provider
	jwtProvider := NewJWTProvider(am.logger)
	_ = am.RegisterProvider(jwtProvider)

	// Register API Key provider
	apiKeyProvider := NewAPIKeyProvider(am.logger)
	_ = am.RegisterProvider(apiKeyProvider)

	// Register Basic Auth provider
	basicProvider := NewBasicAuthProvider(am.logger)
	_ = am.RegisterProvider(basicProvider)
}

// getProvider gets an authentication provider by type
func (am *AuthenticationManager) getProvider(authType AuthType) (AuthProvider, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	provider, exists := am.providers[string(authType)]
	if !exists {
		return nil, fmt.Errorf("no provider found for auth type: %s", authType)
	}

	return provider, nil
}

// getCachedToken retrieves a cached token if valid
func (am *AuthenticationManager) getCachedToken(serverID string) *AuthToken {
	am.mu.RLock()
	defer am.mu.RUnlock()

	cached, exists := am.tokenCache[serverID]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().After(cached.ExpiresAt) {
		// Token expired, will be cleaned up later
		return nil
	}

	return cached.Token
}

// cacheToken caches an authentication token
func (am *AuthenticationManager) cacheToken(serverID string, token *AuthToken) {
	am.mu.Lock()
	defer am.mu.Unlock()

	cached := &CachedToken{
		Token:     token,
		ServerID:  serverID,
		CachedAt:  time.Now(),
		ExpiresAt: token.ExpiresAt,
	}

	am.tokenCache[serverID] = cached
}

// OAuth2Provider implements OAuth2 authentication
type OAuth2Provider struct {
	logger *zap.Logger
}

// NewOAuth2Provider creates a new OAuth2 provider
func NewOAuth2Provider(logger *zap.Logger) *OAuth2Provider {
	return &OAuth2Provider{logger: logger}
}

// GetType returns the authentication type
func (p *OAuth2Provider) GetType() AuthType {
	return AuthOAuth2
}

// Authenticate performs OAuth2 authentication
func (p *OAuth2Provider) Authenticate(ctx context.Context, config AuthConfig) (*AuthToken, error) {
	clientID, ok := config.Config["client_id"]
	if !ok {
		return nil, fmt.Errorf("client_id not found in config")
	}

	clientSecret, ok := config.Config["client_secret"]
	if !ok {
		return nil, fmt.Errorf("client_secret not found in config")
	}

	tokenURL, ok := config.Config["token_url"]
	if !ok {
		return nil, fmt.Errorf("token_url not found in config")
	}

	// Use client credentials flow
	oauthConfig := clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
		Scopes:       strings.Split(config.Config["scopes"], " "),
	}

	token, err := oauthConfig.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("OAuth2 token request failed: %w", err)
	}

	return &AuthToken{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		ExpiresIn:    int64(time.Until(token.Expiry).Seconds()),
		RefreshToken: token.RefreshToken,
		IssuedAt:     time.Now(),
		ExpiresAt:    token.Expiry,
	}, nil
}

// Validate validates an OAuth2 token
func (p *OAuth2Provider) Validate(_ context.Context, _ string) (*TokenInfo, error) {
	// In a real implementation, you would validate the token with the OAuth2 provider
	// This is a simplified implementation
	return &TokenInfo{
		Valid:     true,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

// Refresh refreshes an OAuth2 token
func (p *OAuth2Provider) Refresh(_ context.Context, refreshToken string) (*AuthToken, error) {
	// In a real implementation, you would use the refresh token to get a new access token
	// This is a simplified implementation
	return &AuthToken{
		AccessToken:  "new_access_token",
		TokenType:    "Bearer",
		ExpiresIn:    OAuth2ExpiresIn,
		RefreshToken: refreshToken,
		IssuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
	}, nil
}

// JWTProvider implements JWT authentication
type JWTProvider struct {
	logger *zap.Logger
}

// NewJWTProvider creates a new JWT provider
func NewJWTProvider(logger *zap.Logger) *JWTProvider {
	return &JWTProvider{logger: logger}
}

// GetType returns the authentication type
func (p *JWTProvider) GetType() AuthType {
	return AuthJWT
}

// Authenticate performs JWT authentication
func (p *JWTProvider) Authenticate(_ context.Context, config AuthConfig) (*AuthToken, error) {
	// In a real implementation, you would create or obtain a JWT token
	// This is a simplified implementation
	jwtToken := config.Config["token"]
	if jwtToken == "" {
		return nil, fmt.Errorf("JWT token not found in config")
	}

	return &AuthToken{
		AccessToken: jwtToken,
		TokenType:   "Bearer",
		ExpiresIn:   JWTExpiresIn,
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
	}, nil
}

// Validate validates a JWT token
func (p *JWTProvider) Validate(_ context.Context, token string) (*TokenInfo, error) {
	// In a real implementation, you would parse and validate the JWT
	// This is a simplified implementation
	if token == "" {
		return &TokenInfo{Valid: false}, nil
	}

	return &TokenInfo{
		Valid:     true,
		Subject:   "user",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

// Refresh refreshes a JWT token
func (p *JWTProvider) Refresh(_ context.Context, _ string) (*AuthToken, error) {
	// JWT tokens are typically not refreshable in the same way as OAuth2 tokens
	return nil, fmt.Errorf("JWT token refresh not supported")
}

// APIKeyProvider implements API key authentication
type APIKeyProvider struct {
	logger *zap.Logger
}

// NewAPIKeyProvider creates a new API key provider
func NewAPIKeyProvider(logger *zap.Logger) *APIKeyProvider {
	return &APIKeyProvider{logger: logger}
}

// GetType returns the authentication type
func (p *APIKeyProvider) GetType() AuthType {
	return AuthAPIKey
}

// Authenticate performs API key authentication
func (p *APIKeyProvider) Authenticate(_ context.Context, config AuthConfig) (*AuthToken, error) {
	apiKey, ok := config.Config["key"]
	if !ok {
		return nil, fmt.Errorf("API key not found in config")
	}

	// API keys don't expire by default
	expiresAt := time.Now().Add(DefaultTokenExpiry) // Default 24 hour expiry
	if expiryStr, ok := config.Config["expires_in"]; ok {
		if duration, err := time.ParseDuration(expiryStr); err == nil {
			expiresAt = time.Now().Add(duration)
		}
	}

	return &AuthToken{
		AccessToken: apiKey,
		TokenType:   "ApiKey",
		ExpiresIn:   int64(time.Until(expiresAt).Seconds()),
		IssuedAt:    time.Now(),
		ExpiresAt:   expiresAt,
	}, nil
}

// Validate validates an API key
func (p *APIKeyProvider) Validate(_ context.Context, token string) (*TokenInfo, error) {
	// In a real implementation, you would validate the API key against a database
	// This is a simplified implementation
	if token == "" {
		return &TokenInfo{Valid: false}, nil
	}

	return &TokenInfo{
		Valid:     true,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(DefaultTokenExpiry),
	}, nil
}

// Refresh refreshes an API key
func (p *APIKeyProvider) Refresh(_ context.Context, _ string) (*AuthToken, error) {
	// API keys typically don't have refresh tokens
	return nil, fmt.Errorf("API key refresh not supported")
}

// BasicAuthProvider implements basic authentication
type BasicAuthProvider struct {
	logger *zap.Logger
}

// NewBasicAuthProvider creates a new basic auth provider
func NewBasicAuthProvider(logger *zap.Logger) *BasicAuthProvider {
	return &BasicAuthProvider{logger: logger}
}

// GetType returns the authentication type
func (p *BasicAuthProvider) GetType() AuthType {
	return AuthBasic
}

// Authenticate performs basic authentication
func (p *BasicAuthProvider) Authenticate(_ context.Context, config AuthConfig) (*AuthToken, error) {
	username, ok := config.Config["username"]
	if !ok {
		return nil, fmt.Errorf("username not found in config")
	}

	password, ok := config.Config["password"]
	if !ok {
		return nil, fmt.Errorf("password not found in config")
	}

	// Basic auth doesn't have tokens in the traditional sense
	// We'll encode the credentials as a token for consistency
	credentials := fmt.Sprintf("%s:%s", username, password)

	return &AuthToken{
		AccessToken: credentials,
		TokenType:   "Basic",
		ExpiresIn:   BasicAuthExpiry, // 24 hours
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(DefaultTokenExpiry),
	}, nil
}

// Validate validates basic auth credentials
func (p *BasicAuthProvider) Validate(_ context.Context, token string) (*TokenInfo, error) {
	// In a real implementation, you would validate the credentials against a user store
	// This is a simplified implementation
	if token == "" {
		return &TokenInfo{Valid: false}, nil
	}

	parts := strings.Split(token, ":")
	if len(parts) != BasicAuthParts {
		return &TokenInfo{Valid: false}, nil
	}

	return &TokenInfo{
		Valid:     true,
		Subject:   parts[0], // username
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(DefaultTokenExpiry),
	}, nil
}

// Refresh refreshes basic auth credentials
func (p *BasicAuthProvider) Refresh(_ context.Context, _ string) (*AuthToken, error) {
	// Basic auth doesn't use refresh tokens
	return nil, fmt.Errorf("basic auth refresh not supported")
}
