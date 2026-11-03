package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/reconciler-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/reconciler-service/internal/config"
	"github.com/JoelTeoGom/distributed-scheduler/reconciler-service/internal/service"
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

	jobRepository := mysql.NewJobRepository(db)
	reconcilerService := service.NewReconcilerService(jobRepository, config.ReconcileInterval, config.ReconcileBatchSize)

	reconcilerErrors := make(chan error, 1)
	go func() {
		reconcilerErrors <- reconcilerService.StartReconciler(ctx)
	}()

	select {
	case err := <-reconcilerErrors:
		if err != nil {
			db.Close()
			log.Fatalf("reconciler service: %v", err)
		}
	case <-ctx.Done():
		// A sweep in progress gets the grace period to finish before the database closes.
		select {
		case <-reconcilerErrors:
		case <-time.After(config.ShutdownGracePeriod):
			log.Printf("reconciler did not stop within %s", config.ShutdownGracePeriod)
		}
	}

	if err := db.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
}
