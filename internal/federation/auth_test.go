package federation

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
)

const (
	// Test constants to avoid duplication
	testAPIKey = "test-api-key"
	testKey    = "test-key"
)

func TestNewAuthenticationManager(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	if am == nil {
		t.Fatal("Expected authentication manager instance, got nil")
	}

	if am.providers == nil {
		t.Error("Expected providers map to be initialized")
	}

	if am.tokenCache == nil {
		t.Error("Expected token cache to be initialized")
	}

	// Check that default providers were registered
	if len(am.providers) == 0 {
		t.Error("Expected default providers to be registered")
	}
}

func TestRegisterProvider(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	initialCount := len(am.providers)

	// Create mock provider
	mockProvider := &MockAuthProvider{authType: "mock"}

	err := am.RegisterProvider(mockProvider)
	if err != nil {
		t.Fatalf("Expected successful provider registration: %v", err)
	}

	if len(am.providers) != initialCount+1 {
		t.Error("Expected provider count to increase")
	}

	// Test nil provider
	err = am.RegisterProvider(nil)
	if err == nil {
		t.Error("Expected error for nil provider")
	}
}

func TestGetAuthTokenAPIKey(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	config := AuthConfig{
		Type: AuthAPIKey,
		Config: map[string]string{
			"key": testAPIKey,
		},
	}

	token, err := am.GetAuthToken(context.Background(), "test-server", config)
	if err != nil {
		t.Fatalf("Expected successful authentication: %v", err)
	}

	if token.AccessToken != testAPIKey {
		t.Errorf("Expected access token 'test-api-key', got %s", token.AccessToken)
	}

	if token.TokenType != "ApiKey" {
		t.Errorf("Expected token type 'ApiKey', got %s", token.TokenType)
	}
}

func TestGetAuthTokenBasic(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	config := AuthConfig{
		Type: AuthBasic,
		Config: map[string]string{
			"username": "testuser",
			"password": "testpass",
		},
	}

	token, err := am.GetAuthToken(context.Background(), "test-server", config)
	if err != nil {
		t.Fatalf("Expected successful authentication: %v", err)
	}

	if token.AccessToken != "testuser:testpass" {
		t.Errorf("Expected access token 'testuser:testpass', got %s", token.AccessToken)
	}

	if token.TokenType != "Basic" {
		t.Errorf("Expected token type 'Basic', got %s", token.TokenType)
	}
}

func TestGetAuthTokenJWT(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	config := AuthConfig{
		Type: AuthJWT,
		Config: map[string]string{
			"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test",
		},
	}

	token, err := am.GetAuthToken(context.Background(), "test-server", config)
	if err != nil {
		t.Fatalf("Expected successful authentication: %v", err)
	}

	if token.AccessToken != "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test" {
		t.Errorf("Expected JWT token, got %s", token.AccessToken)
	}

	if token.TokenType != "Bearer" {
		t.Errorf("Expected token type 'Bearer', got %s", token.TokenType)
	}
}

func TestTokenCaching(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	config := AuthConfig{
		Type: AuthAPIKey,
		Config: map[string]string{
			"key": testAPIKey,
		},
	}

	// First request should authenticate
	token1, err := am.GetAuthToken(context.Background(), "test-server", config)
	if err != nil {
		t.Fatalf("Expected successful authentication: %v", err)
	}

	// Second request should return cached token
	token2, err := am.GetAuthToken(context.Background(), "test-server", config)
	if err != nil {
		t.Fatalf("Expected successful authentication from cache: %v", err)
	}

	if token1.AccessToken != token2.AccessToken {
		t.Error("Expected same token from cache")
	}

	// Verify token is actually cached
	if len(am.tokenCache) != 1 {
		t.Errorf("Expected 1 cached token, got %d", len(am.tokenCache))
	}
}

func TestValidateToken(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	// Test API key validation
	info, err := am.ValidateToken(context.Background(), testKey, AuthAPIKey)
	if err != nil {
		t.Fatalf("Expected successful validation: %v", err)
	}

	if !info.Valid {
		t.Error("Expected token to be valid")
	}

	// Test empty token validation
	info, err = am.ValidateToken(context.Background(), "", AuthAPIKey)
	if err != nil {
		t.Fatalf("Expected successful validation: %v", err)
	}

	if info.Valid {
		t.Error("Expected empty token to be invalid")
	}
}

func TestAddAPIKeyAuth(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	req, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
	config := AuthConfig{
		Type: AuthAPIKey,
		Config: map[string]string{
			"key":    "test-api-key",
			"header": "X-Custom-Key",
		},
	}

	err := am.AddAPIKeyAuth(req, config)
	if err != nil {
		t.Fatalf("Expected successful auth addition: %v", err)
	}

	if req.Header.Get("X-Custom-Key") != testAPIKey {
		t.Error("Expected API key header to be set")
	}

	// Test default header
	req2, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
	config2 := AuthConfig{
		Type: AuthAPIKey,
		Config: map[string]string{
			"key": testAPIKey,
		},
	}

	err = am.AddAPIKeyAuth(req2, config2)
	if err != nil {
		t.Fatalf("Expected successful auth addition: %v", err)
	}

	if req2.Header.Get("X-API-Key") != testAPIKey {
		t.Error("Expected default API key header to be set")
	}
}

func TestAddBasicAuth(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	req, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
	config := AuthConfig{
		Type: AuthBasic,
		Config: map[string]string{
			"username": "testuser",
			"password": "testpass",
		},
	}

	err := am.AddBasicAuth(req, config)
	if err != nil {
		t.Fatalf("Expected successful auth addition: %v", err)
	}

	username, password, ok := req.BasicAuth()
	if !ok {
		t.Fatal("Expected basic auth to be set")
	}

	if username != "testuser" || password != "testpass" {
		t.Errorf("Expected username 'testuser' and password 'testpass', got %s:%s", username, password)
	}
}

func TestAddAuthentication(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	// Test AuthNone
	req1, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
	config1 := AuthConfig{Type: AuthNone}

	err := am.AddAuthentication(req1, "server1", config1)
	if err != nil {
		t.Fatalf("Expected successful no-auth addition: %v", err)
	}

	// Test AuthAPIKey
	req2, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
	config2 := AuthConfig{
		Type: AuthAPIKey,
		Config: map[string]string{
			"key": testKey,
		},
	}

	err = am.AddAuthentication(req2, "server2", config2)
	if err != nil {
		t.Fatalf("Expected successful API key auth addition: %v", err)
	}

	if req2.Header.Get("X-API-Key") != testKey {
		t.Error("Expected API key header to be set")
	}

	// Test unsupported auth type
	req3, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
	config3 := AuthConfig{Type: "unsupported"}

	err = am.AddAuthentication(req3, "server3", config3)
	if err == nil {
		t.Error("Expected error for unsupported auth type")
	}
}

func TestCleanupExpiredTokens(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	// Add an expired token
	expiredToken := &CachedToken{
		Token: &AuthToken{
			AccessToken: "expired",
			ExpiresAt:   time.Now().Add(-time.Hour),
		},
		ServerID:  "expired-server",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	am.tokenCache["expired-server"] = expiredToken

	// Add a valid token
	validToken := &CachedToken{
		Token: &AuthToken{
			AccessToken: "valid",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
		ServerID:  "valid-server",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	am.tokenCache["valid-server"] = validToken

	if len(am.tokenCache) != 2 {
		t.Errorf("Expected 2 cached tokens, got %d", len(am.tokenCache))
	}

	// Clean up expired tokens
	am.CleanupExpiredTokens()

	if len(am.tokenCache) != 1 {
		t.Errorf("Expected 1 cached token after cleanup, got %d", len(am.tokenCache))
	}

	if _, exists := am.tokenCache["expired-server"]; exists {
		t.Error("Expected expired token to be removed")
	}

	if _, exists := am.tokenCache["valid-server"]; !exists {
		t.Error("Expected valid token to remain")
	}
}

func TestGetCacheStats(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	// Add tokens
	am.tokenCache["server1"] = &CachedToken{
		ExpiresAt: time.Now().Add(time.Hour),
	}
	am.tokenCache["server2"] = &CachedToken{
		ExpiresAt: time.Now().Add(-time.Hour), // Expired
	}

	stats := am.GetCacheStats()

	if stats["total_tokens"] != 2 {
		t.Errorf("Expected 2 total tokens, got %v", stats["total_tokens"])
	}

	if stats["expired_tokens"] != 1 {
		t.Errorf("Expected 1 expired token, got %v", stats["expired_tokens"])
	}

	if stats["active_tokens"] != 1 {
		t.Errorf("Expected 1 active token, got %v", stats["active_tokens"])
	}
}

func TestMissingConfigValues(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	// Test API key without key
	config1 := AuthConfig{
		Type:   AuthAPIKey,
		Config: map[string]string{},
	}

	_, err := am.GetAuthToken(context.Background(), "test-server", config1)
	if err == nil {
		t.Error("Expected error for API key config without key")
	}

	// Test basic auth without username
	config2 := AuthConfig{
		Type: AuthBasic,
		Config: map[string]string{
			"password": "test",
		},
	}

	_, err = am.GetAuthToken(context.Background(), "test-server", config2)
	if err == nil {
		t.Error("Expected error for basic auth config without username")
	}

	// Test basic auth without password
	config3 := AuthConfig{
		Type: AuthBasic,
		Config: map[string]string{
			"username": "test",
		},
	}

	_, err = am.GetAuthToken(context.Background(), "test-server", config3)
	if err == nil {
		t.Error("Expected error for basic auth config without password")
	}
}

func TestUnsupportedProvider(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	config := AuthConfig{
		Type:   "unsupported",
		Config: map[string]string{},
	}

	_, err := am.GetAuthToken(context.Background(), "test-server", config)
	if err == nil {
		t.Error("Expected error for unsupported auth type")
	}
}

func TestProviderMethods(t *testing.T) {
	logger := zap.NewNop()

	// Test OAuth2 Provider
	oauth2Provider := NewOAuth2Provider(logger)
	if oauth2Provider.GetType() != AuthOAuth2 {
		t.Errorf("Expected OAuth2 provider type, got %s", oauth2Provider.GetType())
	}

	// Test JWT Provider
	jwtProvider := NewJWTProvider(logger)
	if jwtProvider.GetType() != AuthJWT {
		t.Errorf("Expected JWT provider type, got %s", jwtProvider.GetType())
	}

	// Test API Key Provider
	apiKeyProvider := NewAPIKeyProvider(logger)
	if apiKeyProvider.GetType() != AuthAPIKey {
		t.Errorf("Expected API Key provider type, got %s", apiKeyProvider.GetType())
	}

	// Test Basic Auth Provider
	basicProvider := NewBasicAuthProvider(logger)
	if basicProvider.GetType() != AuthBasic {
		t.Errorf("Expected Basic provider type, got %s", basicProvider.GetType())
	}
}

func TestRefreshTokenUnsupported(t *testing.T) {
	logger := zap.NewNop()
	am := NewAuthenticationManager(logger)

	// Test JWT refresh (should fail)
	_, err := am.RefreshToken(context.Background(), "server1", "refresh", AuthJWT)
	if err == nil {
		t.Error("Expected error for JWT refresh (not supported)")
	}

	// Test API Key refresh (should fail)
	_, err = am.RefreshToken(context.Background(), "server2", "refresh", AuthAPIKey)
	if err == nil {
		t.Error("Expected error for API Key refresh (not supported)")
	}

	// Test Basic Auth refresh (should fail)
	_, err = am.RefreshToken(context.Background(), "server3", "refresh", AuthBasic)
	if err == nil {
		t.Error("Expected error for Basic Auth refresh (not supported)")
	}
}

// MockAuthProvider for testing
type MockAuthProvider struct {
	authType AuthType
}

func (m *MockAuthProvider) GetType() AuthType {
	return m.authType
}

func (m *MockAuthProvider) Authenticate(_ context.Context, _ AuthConfig) (*AuthToken, error) {
	return &AuthToken{
		AccessToken: "mock-token",
		TokenType:   "Mock",
		ExpiresIn:   3600,
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
	}, nil
}

func (m *MockAuthProvider) Validate(_ context.Context, token string) (*TokenInfo, error) {
	return &TokenInfo{
		Valid:     token != "",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (m *MockAuthProvider) Refresh(_ context.Context, _ string) (*AuthToken, error) {
	return &AuthToken{
		AccessToken: "refreshed-mock-token",
		TokenType:   "Mock",
		ExpiresIn:   3600,
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
	}, nil
}
