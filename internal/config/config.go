package config

import (
	"fmt"
	"log"
	"os"
)

// Config holds all configuration for the application.
// Values are loaded from environment variables (or .env file in development).
type Config struct {
	// Server
	Port string
	Env  string

	// Database
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// Auth
	JWTSecret string

	// AWS / Media upload
	AWSRegion         string
	S3Bucket          string
	CloudFrontBaseURL string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		Port:       getEnv("PORT", "8080"),
		Env:        getEnv("ENV", "development"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "realdeal"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "realdeal"),
		DBSSLMode:  getEnv("DB_SSL_MODE", "disable"),
		JWTSecret:  getEnv("JWT_SECRET", ""),

		AWSRegion:         getEnv("AWS_REGION", "us-west-2"),
		S3Bucket:          getEnv("S3_BUCKET", ""),
		CloudFrontBaseURL: getEnv("CLOUDFRONT_BASE_URL", ""),
	}

	if cfg.DBPassword == "" {
		return nil, fmt.Errorf("DB_PASSWORD is required")
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	// S3 config is optional — warn if missing so devs know uploads will be unavailable.
	if cfg.S3Bucket == "" {
		log.Println("WARNING: S3_BUCKET is not set — upload endpoint will return 503")
	}
	if cfg.CloudFrontBaseURL == "" {
		log.Println("WARNING: CLOUDFRONT_BASE_URL is not set — upload endpoint will return 503")
	}

	return cfg, nil
}

// DSN returns the PostgreSQL data source name for GORM.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
