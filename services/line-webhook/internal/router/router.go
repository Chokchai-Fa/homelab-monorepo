package router

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/handler"
)

type RouterOptions struct {
	Echo   *echo.Echo
	Config *handler.Config
	Bot    *linebot.Client
}

func NewRouter(opts RouterOptions) *echo.Echo {
	e := opts.Echo
	if e == nil {
		e = echo.New()
	}

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Routes
	h := handler.New(opts.Config, opts.Bot)
	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status":  "ok",
			"message": "LINE Bot Webhook Server is running",
		})
	})

	e.POST("/webhook", h.Webhook, ValidateSignatureMiddleware(opts.Config))

	return e
}

// ValidateSignatureMiddleware validates X-Line-Signature header using the
// configured ChannelSecret. It reads the request body, verifies the HMAC-SHA256
// signature (base64 encoded) and restores the body for downstream handlers.
func ValidateSignatureMiddleware(cfg *handler.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			body, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "Failed to read request body")
			}

			// restore body for downstream
			c.Request().Body = io.NopCloser(bytes.NewReader(body))

			signature := c.Request().Header.Get("X-Line-Signature")
			if signature == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "Missing X-Line-Signature header")
			}

			// Normalize the header and compute expected signature.
			signature = strings.TrimSpace(signature)
			mac := hmac.New(sha256.New, []byte(cfg.ChannelSecret))
			mac.Write(body)
			expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

			// Compare in constant time using hmac.Equal on the byte representations.
			if !hmac.Equal([]byte(signature), []byte(expected)) {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid signature")
			}

			return next(c)
		}
	}
}
