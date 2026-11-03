package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	envFile                    = ".env.local"
	defaultServiceName         = "archiver-service"
	defaultServiceVersion      = "dev"
	defaultShutdownGracePeriod = "30s"
	defaultArchiveInterval     = "1m"
	defaultArchiveBatchSize    = "500"
	defaultSucceededRetention  = "1h"
	defaultDeadRetention       = "336h"
	defaultCassandraHosts      = "127.0.0.1"
	defaultCassandraKeyspace   = "scheduler"
)

type Config struct {
	ServiceName         string
	ServiceVersion      string
	DatabaseDSN         string
	CassandraHosts      []string
	CassandraKeyspace   string
	ShutdownGracePeriod time.Duration
	ArchiveInterval     time.Duration
	ArchiveBatchSize    int
	SucceededRetention  time.Duration
	DeadRetention       time.Duration
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

	archiveInterval, err := time.ParseDuration(getEnvOrDefault("ARCHIVE_INTERVAL", defaultArchiveInterval))
	if err != nil {
		return Config{}, fmt.Errorf("parse ARCHIVE_INTERVAL: %w", err)
	}

	archiveBatchSize, err := strconv.Atoi(getEnvOrDefault("ARCHIVE_BATCH_SIZE", defaultArchiveBatchSize))
	if err != nil {
		return Config{}, fmt.Errorf("parse ARCHIVE_BATCH_SIZE: %w", err)
	}

	succeededRetention, err := time.ParseDuration(getEnvOrDefault("SUCCEEDED_RETENTION", defaultSucceededRetention))
	if err != nil {
		return Config{}, fmt.Errorf("parse SUCCEEDED_RETENTION: %w", err)
	}

	deadRetention, err := time.ParseDuration(getEnvOrDefault("DEAD_RETENTION", defaultDeadRetention))
	if err != nil {
		return Config{}, fmt.Errorf("parse DEAD_RETENTION: %w", err)
	}

	config := Config{
		ServiceName:         getEnvOrDefault("SERVICE_NAME", defaultServiceName),
		ServiceVersion:      getEnvOrDefault("SERVICE_VERSION", defaultServiceVersion),
		DatabaseDSN:         os.Getenv("DATABASE_DSN"),
		CassandraHosts:      strings.Split(getEnvOrDefault("CASSANDRA_HOSTS", defaultCassandraHosts), ","),
		CassandraKeyspace:   getEnvOrDefault("CASSANDRA_KEYSPACE", defaultCassandraKeyspace),
		ShutdownGracePeriod: shutdownGracePeriod,
		ArchiveInterval:     archiveInterval,
		ArchiveBatchSize:    archiveBatchSize,
		SucceededRetention:  succeededRetention,
		DeadRetention:       deadRetention,
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

	if c.ArchiveInterval <= 0 {
		problems = append(problems, errors.New("ARCHIVE_INTERVAL must be positive"))
	}

	if c.ArchiveBatchSize <= 0 {
		problems = append(problems, errors.New("ARCHIVE_BATCH_SIZE must be positive"))
	}

	if c.SucceededRetention < 0 {
		problems = append(problems, errors.New("SUCCEEDED_RETENTION cannot be negative"))
	}

	if c.DeadRetention < 0 {
		problems = append(problems, errors.New("DEAD_RETENTION cannot be negative"))
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
