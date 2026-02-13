// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	JWT      JWTConfig
	App      AppConfig
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	Environment  string
}

// DatabaseConfig contains PostgreSQL connection settings
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
	MaxConns int32
	MinConns int32
}

// RedisConfig contains Redis connection settings
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// JWTConfig contains JWT authentication settings
type JWTConfig struct {
	Secret          string
	AccessDuration  time.Duration
	RefreshDuration time.Duration
}

// AppConfig contains application-specific settings
type AppConfig struct {
	Name                string
	Version             string
	DefaultDailyLimit   float64
	DefaultMonthlyLimit float64
	MintBlockInterval   time.Duration
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:         getEnv("PORT", "8080"),
			ReadTimeout:  getDuration("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout: getDuration("SERVER_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:  getDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
			Environment:  getEnv("ENV", "development"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			Name:     getEnv("DB_NAME", "canglanfu"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
			MaxConns: int32(getInt("DB_MAX_CONNS", 25)),
			MinConns: int32(getInt("DB_MIN_CONNS", 5)),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getInt("REDIS_DB", 0),
		},
		JWT: JWTConfig{
			Secret:          getEnv("JWT_SECRET", "change-me-in-production"),
			AccessDuration:  getDuration("JWT_ACCESS_DURATION", 15*time.Minute),
			RefreshDuration: getDuration("JWT_REFRESH_DURATION", 7*24*time.Hour),
		},
		App: AppConfig{
			Name:                getEnv("APP_NAME", "CangLanFu"),
			Version:             getEnv("APP_VERSION", "1.0.0"),
			DefaultDailyLimit:   getFloat("DEFAULT_DAILY_LIMIT", 1000.0),
			DefaultMonthlyLimit: getFloat("DEFAULT_MONTHLY_LIMIT", 30000.0),
			MintBlockInterval:   getDuration("MINT_BLOCK_INTERVAL", 30*time.Minute),
		},
	}

	// Validate required config
	if cfg.JWT.Secret == "change-me-in-production" && cfg.Server.Environment == "production" {
		return nil, fmt.Errorf("JWT_SECRET must be set in production")
	}

	return cfg, nil
}

// ---------------------------------------------------------------------
// Helper functions to read environment variables with defaults
// ---------------------------------------------------------------------

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getFloat(key string, defaultValue float64) float64 {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return defaultValue
	}
	return value
}

func getDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}
