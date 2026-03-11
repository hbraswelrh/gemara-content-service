package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"
)

// JWTAuthConfig holds the configuration for JWT authentication
type JWTAuthConfig struct {
	IssuerURL           string // OIDC issuer URL (defaults to "https://kubernetes.default.svc" if empty)
	KubernetesServiceIP string // Kubernetes API IP for DNS bypass (falls back to env vars if empty)
	ExpectedAudience    string
	AllowedSubjects     []string
}

// JWTAuth holds the initialized OIDC verifier and configuration for JWT authentication
type JWTAuth struct {
	verifier        *oidc.IDTokenVerifier
	httpClient      *http.Client
	allowedSubjects []string
}

// NewJWTAuth initializes a new JWTAuth instance with the given configuration.
// This performs all OIDC provider setup and returns an error if initialization fails.
func NewJWTAuth(ctx context.Context, config JWTAuthConfig) (*JWTAuth, error) {
	// 1. Get in-cluster Kubernetes config
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("failed to get in-cluster config", "error", err)
		k8sConfig = &rest.Config{
			Host: "https://kubernetes.default.svc",
		}
	}

	// 2. Determine the Kubernetes API IP for DNS bypass
	kubernetesServiceIP := config.KubernetesServiceIP
	if kubernetesServiceIP == "" {
		kubernetesServiceIP = os.Getenv("KUBERNETES_SERVICE_IP")
	}
	dnsBypassEnabled := kubernetesServiceIP != ""

	// 3. Apply aggressive DNS bypass
	if dnsBypassEnabled {
		slog.Info("DNS bypass enabled - forcing connections to internal Kubernetes API IP", "kubernetes_ip", kubernetesServiceIP)

		k8sConfig.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			forcedAddress := net.JoinHostPort(kubernetesServiceIP, "443")

			slog.Debug("DNS bypass intercepted connection", "original_addr", address, "forced_addr", forcedAddress)

			dialer := &net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, forcedAddress)
		}
	}

	// 4. Create HTTP client using Kubernetes configuration
	httpClient, err := rest.HTTPClientFor(k8sConfig)
	if err != nil {
		slog.Error("failed to create HTTP client", "error", err)
		httpClient = http.DefaultClient
	}

	issuerURL := config.IssuerURL
	if issuerURL == "" {
		issuerURL = "https://kubernetes.default.svc"
	}

	// Startup context used strictly for initial provider setup
	startupCtx := oidc.ClientContext(context.Background(), httpClient)

	// Startup retry logic
	var provider *oidc.Provider
	backoffDurations := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 1; attempt <= 3; attempt++ {
		provider, err = oidc.NewProvider(startupCtx, issuerURL)
		if err == nil {
			slog.Info("successfully initialized OIDC provider", "attempt", attempt)
			break
		}

		slog.Warn("failed to initialize OIDC provider on startup", "attempt", attempt, "error", err)
		if attempt < 3 {
			time.Sleep(backoffDurations[attempt-1])
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider after retries: %w", err)
	}

	verifierConfig := &oidc.Config{
		ClientID: config.ExpectedAudience,
	}

	if dnsBypassEnabled {
		verifierConfig.SkipIssuerCheck = true
		slog.Info("OIDC issuer validation disabled due to DNS bypass")
	}

	verifier := provider.Verifier(verifierConfig)

	slog.Info("JWT authentication initialized",
		"issuer", issuerURL,
		"audience", config.ExpectedAudience,
		"dns_bypass", dnsBypassEnabled)

	return &JWTAuth{
		verifier:        verifier,
		httpClient:      httpClient,
		allowedSubjects: config.AllowedSubjects,
	}, nil
}

// Middleware returns a Gin middleware handler that validates JWT tokens
func (j *JWTAuth) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)

		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		rawToken := parts[1]

		// Create a request-scoped context containing the custom HTTP Client
		reqCtx := oidc.ClientContext(c.Request.Context(), j.httpClient)

		// Verify the token validation(cryptographic signature and expiration)
		idToken, err := j.verifier.Verify(reqCtx, rawToken)
		if err != nil {
			slog.Warn("token verification failed", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		// Decode the token's payload (claims) into a Go map
		var claims map[string]interface{}
		if err := idToken.Claims(&claims); err != nil {
			slog.Warn("failed to extract claims", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "failed to extract token claims"})
			return
		}

		// Verify the caller's identity (Subject/Service Account)
		if len(j.allowedSubjects) > 0 {
			subject, ok := claims["sub"].(string)
			if !ok {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "subject claim missing"})
				return
			}

			if err := validateSubject(subject, j.allowedSubjects); err != nil {
				slog.Warn("subject validation failed", "error", err, "subject", subject)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
		}

		// Save the parsed claims into the Gin context so the /v1/enrich can know exactly who is making the request.
		c.Set("jwt_claims", claims)

		c.Next()
	}
}

// JWTAuthMiddleware creates a Gin middleware that validates bound service account tokens.
// Deprecated: Use NewJWTAuth followed by Middleware() for better error handling and testability.
func JWTAuthMiddleware(config JWTAuthConfig) gin.HandlerFunc {
	auth, err := NewJWTAuth(context.Background(), config)
	if err != nil {
		slog.Error("fatal: failed to initialize JWT authentication", "error", err)
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "JWT authentication initialization failed",
			})
		}
	}
	return auth.Middleware()
}

func validateSubject(subject string, allowedSubjects []string) error {
	for _, allowed := range allowedSubjects {
		if subject == allowed {
			return nil
		}
	}
	return fmt.Errorf("subject %q not in allowed list", subject)
}
