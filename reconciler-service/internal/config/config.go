package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const (
	envFile                    = ".env.local"
	defaultServiceName         = "reconciler-service"
	defaultServiceVersion      = "dev"
	defaultShutdownGracePeriod = "30s"
	defaultReconcileInterval   = "1m"
	defaultReconcileBatchSize  = "1000"
)

type Config struct {
	ServiceName         string
	ServiceVersion      string
	DatabaseDSN         string
	ShutdownGracePeriod time.Duration
	ReconcileInterval   time.Duration
	ReconcileBatchSize  int
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

	reconcileInterval, err := time.ParseDuration(getEnvOrDefault("RECONCILE_INTERVAL", defaultReconcileInterval))
	if err != nil {
		return Config{}, fmt.Errorf("parse RECONCILE_INTERVAL: %w", err)
	}

	reconcileBatchSize, err := strconv.Atoi(getEnvOrDefault("RECONCILE_BATCH_SIZE", defaultReconcileBatchSize))
	if err != nil {
		return Config{}, fmt.Errorf("parse RECONCILE_BATCH_SIZE: %w", err)
	}

	config := Config{
		ServiceName:         getEnvOrDefault("SERVICE_NAME", defaultServiceName),
		ServiceVersion:      getEnvOrDefault("SERVICE_VERSION", defaultServiceVersion),
		DatabaseDSN:         os.Getenv("DATABASE_DSN"),
		ShutdownGracePeriod: shutdownGracePeriod,
		ReconcileInterval:   reconcileInterval,
		ReconcileBatchSize:  reconcileBatchSize,
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

	if c.ShutdownGracePeriod <= 0 {
		problems = append(problems, errors.New("SHUTDOWN_GRACE_PERIOD must be positive"))
	}

	if c.ReconcileInterval <= 0 {
		problems = append(problems, errors.New("RECONCILE_INTERVAL must be positive"))
	}

	if c.ReconcileBatchSize <= 0 {
		problems = append(problems, errors.New("RECONCILE_BATCH_SIZE must be positive"))
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
