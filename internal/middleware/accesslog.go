package middleware

import (
	"log/slog"
	"time"

	requestid "github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

// AccessLogger emits a structured log line per request after it is handled.
func AccessLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		rid := requestid.Get(c)
		latency := time.Since(start)
		path := c.Request.URL.Path
		rawQuery := c.Request.URL.RawQuery
		if rawQuery != "" {
			path = path + "?" + rawQuery
		}

		slog.Info("http_request", //nolint:gosec // G706 - structured slog attributes prevent log injection
			slog.String("request_id", rid),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Int("size", c.Writer.Size()),
			slog.String("ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
			slog.Duration("latency", latency),
		)
	}
}
