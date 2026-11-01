package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	envFile = ".env.local"

	defaultServiceName         = "task-service"
	defaultServiceVersion      = "dev"
	defaultServerAddress       = ":8080"
	defaultShutdownGracePeriod = "30s"

	minJWTSecretLength = 32
)

type Config struct {
	ServiceName         string
	ServiceVersion      string
	ServerAddress       string
	DatabaseDSN         string
	JWTSecret           string
	ShutdownGracePeriod time.Duration
}

// Variables already set in the environment win over .env.local, so a container needs no file.
func Load() (Config, error) {
	if err := godotenv.Load(envFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("load %s: %w", envFile, err)
	}

	shutdownGracePeriod, err := time.ParseDuration(getEnvOrDefault("SHUTDOWN_GRACE_PERIOD", defaultShutdownGracePeriod))
	if err != nil {
		return Config{}, fmt.Errorf("parse SHUTDOWN_GRACE_PERIOD: %w", err)
	}

	config := Config{
		ServiceName:         getEnvOrDefault("SERVICE_NAME", defaultServiceName),
		ServiceVersion:      getEnvOrDefault("SERVICE_VERSION", defaultServiceVersion),
		ServerAddress:       getEnvOrDefault("SERVER_ADDRESS", defaultServerAddress),
		DatabaseDSN:         os.Getenv("DATABASE_DSN"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		ShutdownGracePeriod: shutdownGracePeriod,
	}
	if err := config.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return config, nil
}

func (c Config) validate() error {
	var problems []error
	if c.DatabaseDSN == "" {
		problems = append(problems, errors.New("DATABASE_DSN is required"))
	}
	if len(c.JWTSecret) < minJWTSecretLength {
		problems = append(problems, fmt.Errorf("JWT_SECRET must be at least %d bytes", minJWTSecretLength))
	}
	if _, _, err := net.SplitHostPort(c.ServerAddress); err != nil {
		problems = append(problems, fmt.Errorf("SERVER_ADDRESS: %w", err))
	}
	if c.ShutdownGracePeriod <= 0 {
		problems = append(problems, errors.New("SHUTDOWN_GRACE_PERIOD must be positive"))
	}
	return errors.Join(problems...)
}

func getEnvOrDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
