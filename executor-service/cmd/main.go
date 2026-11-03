package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/adapters/out/httpclient"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/config"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	config, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := mysql.Open(ctx, config.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	jobRepository := mysql.NewJobRepository(db, config.ExecutorID)
	if config.AllowPrivateDestinations {
		log.Printf("ALLOW_PRIVATE_DESTINATIONS is on: jobs can call internal addresses, never use it in production")
	}
	executor := httpclient.NewExecutor(config.AllowPrivateDestinations)
	workerService := service.NewWorkerService(jobRepository, executor, config.JobQueueSize, config.JobTimeLimitSeconds)

	// The pool runs until the signal, and the database closes after it so in-flight jobs can save their result.
	if err := workerService.StartWorkerPool(ctx, config.WorkerCount); err != nil && !errors.Is(err, context.Canceled) {
		db.Close()
		log.Fatalf("worker pool: %v", err)
	}

	if err := db.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
}
