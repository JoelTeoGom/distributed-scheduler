package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	envFile                    = ".env.local"
	defaultServiceName         = "scheduler-service"
	defaultServiceVersion      = "dev"
	defaultShutdownGracePeriod = "30s"
	defaultScheduleInterval    = "5s"
	defaultScheduleLookahead   = "30s"
)

type Config struct {
	ServiceName         string
	ServiceVersion      string
	DatabaseDSN         string
	ShutdownGracePeriod time.Duration
	ScheduleInterval    time.Duration
	ScheduleLookahead   time.Duration
	// Serves Go's pprof on this address when set, for profiling. Off by default.
	PprofAddr string
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

	scheduleInterval, err := time.ParseDuration(getEnvOrDefault("SCHEDULE_INTERVAL", defaultScheduleInterval))
	if err != nil {
		return Config{}, fmt.Errorf("parse SCHEDULE_INTERVAL: %w", err)
	}

	scheduleLookahead, err := time.ParseDuration(getEnvOrDefault("SCHEDULE_LOOKAHEAD", defaultScheduleLookahead))
	if err != nil {
		return Config{}, fmt.Errorf("parse SCHEDULE_LOOKAHEAD: %w", err)
	}

	config := Config{
		ServiceName:         getEnvOrDefault("SERVICE_NAME", defaultServiceName),
		ServiceVersion:      getEnvOrDefault("SERVICE_VERSION", defaultServiceVersion),
		DatabaseDSN:         os.Getenv("DATABASE_DSN"),
		ShutdownGracePeriod: shutdownGracePeriod,
		ScheduleInterval:    scheduleInterval,
		ScheduleLookahead:   scheduleLookahead,
		PprofAddr:           os.Getenv("PPROF_ADDR"),
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

	if c.ScheduleInterval <= 0 {
		problems = append(problems, errors.New("SCHEDULE_INTERVAL must be positive"))
	}

	if c.ScheduleLookahead <= 0 {
		problems = append(problems, errors.New("SCHEDULE_LOOKAHEAD must be positive"))
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
