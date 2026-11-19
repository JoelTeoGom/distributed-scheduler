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
	defaultWorkerCount         = "50"
	defaultAllowPrivate        = "false"
	defaultMaxOpenConnections  = "5"
	defaultMaxIdleConnections  = "5"
)

type Config struct {
	ServiceName         string
	ServiceVersion      string
	ExecutorID          string
	DatabaseDSN         string
	ShutdownGracePeriod time.Duration
	JobQueueSize        int
	JobTimeLimitSeconds int
	// How often the pool claims, and how far ahead of their time it takes jobs. Both default to JOB_TIME_LIMIT_SECONDS.
	ClaimInterval time.Duration
	ClaimLead     time.Duration
	WorkerCount   int
	// Turns the SSRF check off so local tests can call endpoints on localhost.
	AllowPrivateDestinations bool
	// Serves Go's pprof on this address when set, for profiling. Off by default.
	PprofAddr string
	// Claims and result saves share this pool, so it bounds how many run at once.
	MaxOpenConnections int
	MaxIdleConnections int
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

	workerCount, err := strconv.Atoi(getEnvOrDefault("WORKER_COUNT", defaultWorkerCount))
	if err != nil {
		return Config{}, fmt.Errorf("parse WORKER_COUNT: %w", err)
	}

	allowPrivateDestinations, err := strconv.ParseBool(getEnvOrDefault("ALLOW_PRIVATE_DESTINATIONS", defaultAllowPrivate))
	if err != nil {
		return Config{}, fmt.Errorf("parse ALLOW_PRIVATE_DESTINATIONS: %w", err)
	}

	claimInterval, err := parseDurationOr(os.Getenv("CLAIM_INTERVAL"), time.Duration(jobTimeLimitSeconds)*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("parse CLAIM_INTERVAL: %w", err)
	}

	claimLead, err := parseDurationOr(os.Getenv("CLAIM_LEAD"), time.Duration(jobTimeLimitSeconds)*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("parse CLAIM_LEAD: %w", err)
	}

	maxOpenConnections, err := strconv.Atoi(getEnvOrDefault("MYSQL_MAX_OPEN_CONNS", defaultMaxOpenConnections))
	if err != nil {
		return Config{}, fmt.Errorf("parse MYSQL_MAX_OPEN_CONNS: %w", err)
	}

	maxIdleConnections, err := strconv.Atoi(getEnvOrDefault("MYSQL_MAX_IDLE_CONNS", defaultMaxIdleConnections))
	if err != nil {
		return Config{}, fmt.Errorf("parse MYSQL_MAX_IDLE_CONNS: %w", err)
	}

	executorID := os.Getenv("EXECUTOR_ID")
	if executorID == "" {
		// In Kubernetes the hostname is the pod name.
		if executorID, err = os.Hostname(); err != nil {
			return Config{}, fmt.Errorf("read hostname: %w", err)
		}
	}

	config := Config{
		ServiceName:              getEnvOrDefault("SERVICE_NAME", defaultServiceName),
		ExecutorID:               executorID,
		ServiceVersion:           getEnvOrDefault("SERVICE_VERSION", defaultServiceVersion),
		DatabaseDSN:              os.Getenv("DATABASE_DSN"),
		ShutdownGracePeriod:      shutdownGracePeriod,
		JobQueueSize:             jobQueueSize,
		JobTimeLimitSeconds:      jobTimeLimitSeconds,
		ClaimInterval:            claimInterval,
		ClaimLead:                claimLead,
		WorkerCount:              workerCount,
		AllowPrivateDestinations: allowPrivateDestinations,
		PprofAddr:                os.Getenv("PPROF_ADDR"),
		MaxOpenConnections:       maxOpenConnections,
		MaxIdleConnections:       maxIdleConnections,
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

	if len(c.ExecutorID) > 64 {
		problems = append(problems, errors.New("EXECUTOR_ID must be at most 64 characters"))
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

	if c.ClaimInterval <= 0 {
		problems = append(problems, errors.New("CLAIM_INTERVAL must be positive"))
	}

	if c.ClaimLead <= 0 {
		problems = append(problems, errors.New("CLAIM_LEAD must be positive"))
	}

	if c.WorkerCount <= 0 {
		problems = append(problems, errors.New("WORKER_COUNT must be positive"))
	}

	if c.MaxOpenConnections <= 0 {
		problems = append(problems, errors.New("MYSQL_MAX_OPEN_CONNS must be positive"))
	}

	if c.MaxIdleConnections < 0 || c.MaxIdleConnections > c.MaxOpenConnections {
		problems = append(problems, errors.New("MYSQL_MAX_IDLE_CONNS must be between 0 and MYSQL_MAX_OPEN_CONNS"))
	}
	return errors.Join(problems...)
}

func parseDurationOr(value string, defaultValue time.Duration) (time.Duration, error) {
	if value == "" {
		return defaultValue, nil
	}
	return time.ParseDuration(value)
}

func getEnvOrDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
