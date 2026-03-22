package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/hemantkpr/mailmate/internal/config"
	"github.com/hemantkpr/mailmate/internal/handler"
	"github.com/hemantkpr/mailmate/internal/middleware"
	"github.com/hemantkpr/mailmate/internal/provider"
	"github.com/hemantkpr/mailmate/internal/service"
	"github.com/hemantkpr/mailmate/internal/store"
)

// Server wraps the HTTP server and all dependencies.
type Server struct {
	httpServer   *http.Server
	notification *service.Notification
	logger       *zap.Logger
}

// New creates and wires all dependencies, returning a ready-to-start Server.
func New(cfg *config.Config, logger *zap.Logger) (*Server, error) {
	ctx := context.Background()

	// --- Database ---
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	poolCfg.MaxConns = int32(cfg.Database.MaxConns)

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	logger.Info("connected to database")

	// --- Encryption ---
	encryptor, err := store.NewEncryptor(cfg.Encryption.Key)
	if err != nil {
		return nil, fmt.Errorf("create encryptor: %w", err)
	}

	// --- Store ---
	db := store.NewPostgres(pool, encryptor, logger)

	// --- Redis ---
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	logger.Info("connected to redis")

	// --- Providers ---
	googleProvider := provider.NewGoogle(cfg.Google)
	microsoftProvider := provider.NewMicrosoft(cfg.Microsoft)
	whatsappProvider := provider.NewWhatsApp(cfg.Twilio)
	openaiProvider := provider.NewOpenAI(cfg.OpenAI)

	// --- Services ---
	authService := service.NewAuth(
		googleProvider, microsoftProvider,
		db, db, rdb, whatsappProvider,
		logger, cfg.Server.BaseURL,
	)

	trackerService := service.NewTracker(db, logger)

	chatbotService := service.NewChatbot(
		authService, trackerService,
		googleProvider, microsoftProvider,
		whatsappProvider, openaiProvider,
		db, db, db, logger,
	)

	notificationService := service.NewNotification(
		authService, trackerService,
		googleProvider, microsoftProvider,
		whatsappProvider, db, db, db, db, logger,
	)

	// --- Handlers ---
	webhookHandler := handler.NewWebhook(chatbotService, logger)
	oauthHandler := handler.NewOAuth(authService, logger)
	healthHandler := handler.NewHealth(db)

	// --- Router ---
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Global middleware
	router.Use(
		middleware.RequestID(),
		middleware.Logger(logger),
		middleware.Recovery(logger),
		middleware.SecureHeaders(),
		middleware.RateLimiter(60),
	)

	// Health endpoints (no auth)
	router.GET("/healthz", healthHandler.Liveness)
	router.GET("/readyz", healthHandler.Readiness)

	// WhatsApp webhook (with Twilio signature validation)
	webhookURL := middleware.FormatWebhookURL(cfg.Server.BaseURL, "/webhook/whatsapp")
	webhookGroup := router.Group("/webhook")
	webhookGroup.Use(middleware.ValidateTwilioSignature(cfg.Twilio.AuthToken, webhookURL))
	webhookGroup.POST("/whatsapp", webhookHandler.HandleIncoming)

	// OAuth callbacks (public, with CORS for browser redirects)
	oauthGroup := router.Group("/oauth")
	oauthGroup.Use(middleware.CORS())
	oauthGroup.GET("/google/callback", oauthHandler.GoogleCallback)
	oauthGroup.GET("/microsoft/callback", oauthHandler.MicrosoftCallback)

	// --- HTTP Server ---
	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	return &Server{
		httpServer:   httpServer,
		notification: notificationService,
		logger:       logger,
	}, nil
}

// Start begins serving HTTP requests and starts the notification scheduler.
func (s *Server) Start() error {
	if err := s.notification.Start(); err != nil {
		return fmt.Errorf("start notifications: %w", err)
	}

	s.logger.Info("server starting", zap.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.notification.Stop()
	return s.httpServer.Shutdown(ctx)
}
