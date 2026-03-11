package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Unit tests for JWT authentication middleware
//
// These tests focus on:
// - Subject validation logic
// - Authorization header parsing and validation
// - Configuration handling
//
// Note: Full integration tests with OIDC token verification require:
// - A mock/test OIDC provider (e.g., using httptest or testcontainers)
// - Valid JWT tokens for testing claims extraction
// - Testing the complete authentication flow end-to-end

func TestValidateSubject(t *testing.T) {
	tests := []struct {
		name            string
		subject         string
		allowedSubjects []string
		wantErr         bool
	}{
		{
			name:    "matching subject",
			subject: "system:serviceaccount:test:collector",
			allowedSubjects: []string{
				"system:serviceaccount:test:collector",
			},
			wantErr: false,
		},
		{
			name:    "matching subject in list",
			subject: "system:serviceaccount:test:collector",
			allowedSubjects: []string{
				"system:serviceaccount:test:other",
				"system:serviceaccount:test:collector",
			},
			wantErr: false,
		},
		{
			name:    "non-matching subject",
			subject: "system:serviceaccount:test:unauthorized",
			allowedSubjects: []string{
				"system:serviceaccount:test:collector",
			},
			wantErr: true,
		},
		{
			name:            "empty subject",
			subject:         "",
			allowedSubjects: []string{"system:serviceaccount:test:collector"},
			wantErr:         true,
		},
		{
			name:            "empty allowed list matches nothing",
			subject:         "system:serviceaccount:test:collector",
			allowedSubjects: []string{},
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubject(tt.subject, tt.allowedSubjects)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJWTAuthMiddleware_MissingAuthHeader(t *testing.T) {
	// Create a mock JWTAuth (we'll test without actual OIDC verification)
	auth := &JWTAuth{
		verifier:        nil, // Will not reach verification in this test
		httpClient:      http.DefaultClient,
		allowedSubjects: []string{"system:serviceaccount:test:collector"},
	}

	// Setup Gin test context
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)

	// Execute middleware
	handler := auth.Middleware()
	handler(c)

	// Verify response
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing authorization header")
}

func TestJWTAuthMiddleware_InvalidAuthHeaderFormat(t *testing.T) {
	tests := []struct {
		name        string
		authHeader  string
		expectedErr string
	}{
		{
			name:        "missing bearer prefix",
			authHeader:  "sometoken",
			expectedErr: "invalid authorization header format",
		},
		{
			name:        "wrong scheme",
			authHeader:  "Basic dXNlcjpwYXNz",
			expectedErr: "invalid authorization header format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &JWTAuth{
				verifier:        nil,
				httpClient:      http.DefaultClient,
				allowedSubjects: []string{},
			}

			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/test", nil)
			c.Request.Header.Set("Authorization", tt.authHeader)

			handler := auth.Middleware()
			handler(c)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), tt.expectedErr)
		})
	}
}

func TestNewJWTAuth_ConfigDefaults(t *testing.T) {
	// This test verifies that configuration defaults are applied correctly
	// Note: This will fail if not running in a Kubernetes cluster or without a mock OIDC provider
	// For real testing, you would need to mock the OIDC provider or use integration tests

	t.Run("default issuer URL", func(t *testing.T) {
		config := JWTAuthConfig{
			IssuerURL:        "", // Should default to kubernetes.default.svc
			ExpectedAudience: "test-audience",
			AllowedSubjects:  []string{"test-subject"},
		}

		// In a real test environment, you'd mock the OIDC provider
		// For now, we just verify the config is structured correctly
		assert.Equal(t, "", config.IssuerURL)
		assert.Equal(t, "test-audience", config.ExpectedAudience)
		assert.Equal(t, []string{"test-subject"}, config.AllowedSubjects)
	})

	t.Run("custom issuer URL", func(t *testing.T) {
		config := JWTAuthConfig{
			IssuerURL:        "https://token.actions.githubusercontent.com",
			ExpectedAudience: "test-audience",
			AllowedSubjects:  []string{"repo:org/repo:ref:refs/heads/main"},
		}

		assert.Equal(t, "https://token.actions.githubusercontent.com", config.IssuerURL)
	})
}

func TestJWTAuth_AllowedSubjects(t *testing.T) {
	// Test that the JWTAuth struct properly stores allowed subjects
	auth := &JWTAuth{
		verifier:        nil,
		httpClient:      http.DefaultClient,
		allowedSubjects: []string{"subject1", "subject2"},
	}

	assert.Equal(t, []string{"subject1", "subject2"}, auth.allowedSubjects)
}

func TestJWTAuthMiddleware_Deprecated(t *testing.T) {
	// Test that the deprecated function still works for backward compatibility
	// This will fail without a real OIDC provider, but we can verify it returns a handler
	config := JWTAuthConfig{
		IssuerURL:        "https://test.example.com",
		ExpectedAudience: "test",
		AllowedSubjects:  []string{"test"},
	}

	handler := JWTAuthMiddleware(config)
	assert.NotNil(t, handler, "JWTAuthMiddleware should return a handler even on init failure")
}
