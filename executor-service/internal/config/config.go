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
	defaultServiceName         = "executor-service"
	defaultServiceVersion      = "dev"
	defaultShutdownGracePeriod = "30s"
	defaultJobQueueSize        = "1000"
	defaultJobTimeLimitSeconds = "10"
)

type Config struct {
	ServiceName         string
	ServiceVersion      string
	DatabaseDSN         string
	ShutdownGracePeriod time.Duration
	JobQueueSize        int
	JobTimeLimitSeconds int
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

	jobQueueSize, err := strconv.Atoi(getEnvOrDefault("JOB_QUEUE_SIZE", defaultJobQueueSize))
	if err != nil {
		return Config{}, fmt.Errorf("parse JOB_QUEUE_SIZE: %w", err)
	}

	jobTimeLimitSeconds, err := strconv.Atoi(getEnvOrDefault("JOB_TIME_LIMIT_SECONDS", defaultJobTimeLimitSeconds))
	if err != nil {
		return Config{}, fmt.Errorf("parse JOB_TIME_LIMIT_SECONDS: %w", err)
	}

	config := Config{
		ServiceName:         getEnvOrDefault("SERVICE_NAME", defaultServiceName),
		ServiceVersion:      getEnvOrDefault("SERVICE_VERSION", defaultServiceVersion),
		DatabaseDSN:         os.Getenv("DATABASE_DSN"),
		ShutdownGracePeriod: shutdownGracePeriod,
		JobQueueSize:        jobQueueSize,
		JobTimeLimitSeconds: jobTimeLimitSeconds,
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

	if c.JobQueueSize < 0 {
		problems = append(problems, errors.New("JOB_QUEUE_SIZE cannot be negative"))
	}

	if c.JobTimeLimitSeconds <= 0 {
		problems = append(problems, errors.New("JOB_TIME_LIMIT_SECONDS must be positive"))
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
