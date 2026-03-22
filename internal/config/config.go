package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server     ServerConfig
	Database   DatabaseConfig
	Redis      RedisConfig
	Google     GoogleConfig
	Microsoft  MicrosoftConfig
	Twilio     TwilioConfig
	OpenAI     OpenAIConfig
	Encryption EncryptionConfig
	Log        LogConfig
}

type ServerConfig struct {
	Port         string
	Host         string
	BaseURL      string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
	MaxConns int
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type MicrosoftConfig struct {
	ClientID     string
	ClientSecret string
	TenantID     string
	RedirectURL  string
}

type TwilioConfig struct {
	AccountSID   string
	AuthToken    string
	WhatsAppFrom string
}

type OpenAIConfig struct {
	APIKey string
	Model  string
}

type EncryptionConfig struct {
	Key string
}

type LogConfig struct {
	Level  string
	Format string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	baseURL := mustGetEnv("SERVER_BASE_URL")

	cfg := &Config{
		Server: ServerConfig{
			Port:         getEnv("SERVER_PORT", "8080"),
			Host:         getEnv("SERVER_HOST", "0.0.0.0"),
			BaseURL:      baseURL,
			ReadTimeout:  getDurationEnv("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getDurationEnv("SERVER_WRITE_TIMEOUT", 30*time.Second),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     mustGetEnv("DB_USER"),
			Password: mustGetEnv("DB_PASSWORD"),
			Name:     getEnv("DB_NAME", "mailmate"),
			SSLMode:  getEnv("DB_SSL_MODE", "require"),
			MaxConns: getIntEnv("DB_MAX_CONNS", 25),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getIntEnv("REDIS_DB", 0),
		},
		Google: GoogleConfig{
			ClientID:     mustGetEnv("GOOGLE_CLIENT_ID"),
			ClientSecret: mustGetEnv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  baseURL + "/oauth/google/callback",
		},
		Microsoft: MicrosoftConfig{
			ClientID:     mustGetEnv("MICROSOFT_CLIENT_ID"),
			ClientSecret: mustGetEnv("MICROSOFT_CLIENT_SECRET"),
			TenantID:     getEnv("MICROSOFT_TENANT_ID", "common"),
			RedirectURL:  baseURL + "/oauth/microsoft/callback",
		},
		Twilio: TwilioConfig{
			AccountSID:   mustGetEnv("TWILIO_ACCOUNT_SID"),
			AuthToken:    mustGetEnv("TWILIO_AUTH_TOKEN"),
			WhatsAppFrom: mustGetEnv("TWILIO_WHATSAPP_FROM"),
		},
		OpenAI: OpenAIConfig{
			APIKey: mustGetEnv("OPENAI_API_KEY"),
			Model:  getEnv("OPENAI_MODEL", "gpt-4o"),
		},
		Encryption: EncryptionConfig{
			Key: mustGetEnv("ENCRYPTION_KEY"),
		},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %s is not set", key))
	}
	return v
}

func getIntEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
