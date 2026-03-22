package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/hemantkpr/mailmate/internal/service"
)

// OAuth handles OAuth callback endpoints.
type OAuth struct {
	auth   *service.Auth
	logger *zap.Logger
}

// NewOAuth creates an OAuth handler.
func NewOAuth(auth *service.Auth, logger *zap.Logger) *OAuth {
	return &OAuth{auth: auth, logger: logger}
}

// GoogleCallback handles the Google OAuth2 callback.
func (o *OAuth) GoogleCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errParam := c.Query("error")

	if errParam != "" {
		o.logger.Warn("google oauth error", zap.String("error", errParam))
		c.HTML(http.StatusOK, "", oauthResultPage("Google", false, "Authorization was denied."))
		return
	}

	if code == "" || state == "" {
		c.HTML(http.StatusBadRequest, "", oauthResultPage("Google", false, "Invalid callback parameters."))
		return
	}

	if err := o.auth.HandleGoogleCallback(c.Request.Context(), code, state); err != nil {
		o.logger.Error("google oauth callback", zap.Error(err))
		c.HTML(http.StatusOK, "", oauthResultPage("Google", false, "Failed to connect. The link may have expired. Please try again."))
		return
	}

	c.HTML(http.StatusOK, "", oauthResultPage("Google", true, ""))
}

// MicrosoftCallback handles the Microsoft OAuth2 callback.
func (o *OAuth) MicrosoftCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errParam := c.Query("error")

	if errParam != "" {
		o.logger.Warn("microsoft oauth error", zap.String("error", errParam))
		c.HTML(http.StatusOK, "", oauthResultPage("Microsoft", false, "Authorization was denied."))
		return
	}

	if code == "" || state == "" {
		c.HTML(http.StatusBadRequest, "", oauthResultPage("Microsoft", false, "Invalid callback parameters."))
		return
	}

	if err := o.auth.HandleMicrosoftCallback(c.Request.Context(), code, state); err != nil {
		o.logger.Error("microsoft oauth callback", zap.Error(err))
		c.HTML(http.StatusOK, "", oauthResultPage("Microsoft", false, "Failed to connect. The link may have expired. Please try again."))
		return
	}

	c.HTML(http.StatusOK, "", oauthResultPage("Microsoft", true, ""))
}

func oauthResultPage(provider string, success bool, errMsg string) string {
	title := provider + " Connected!"
	message := "✅ Your " + provider + " account has been connected to MailMate. You can close this window and return to WhatsApp."
	color := "#4CAF50"

	if !success {
		title = "Connection Failed"
		message = "❌ " + errMsg
		color = "#f44336"
	}

	return `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>MailMate - ` + title + `</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex; justify-content: center; align-items: center;
            min-height: 100vh; margin: 0;
            background: #f5f5f5; color: #333;
        }
        .card {
            background: white; padding: 2rem; border-radius: 12px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            text-align: center; max-width: 400px;
        }
        .icon { font-size: 3rem; margin-bottom: 1rem; }
        h1 { color: ` + color + `; margin: 0.5rem 0; }
        p { color: #666; line-height: 1.6; }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">` + func() string {
		if success {
			return "🎉"
		}
		return "😞"
	}() + `</div>
        <h1>` + title + `</h1>
        <p>` + message + `</p>
    </div>
</body>
</html>`
}
