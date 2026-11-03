package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/adapters/out/cassandra"
	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/config"
	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/service"
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

	session, err := cassandra.Open(config.CassandraHosts, config.CassandraKeyspace)
	if err != nil {
		db.Close()
		log.Fatalf("open cassandra: %v", err)
	}

	jobRepository := mysql.NewJobRepository(db)
	jobHistoryRepository := cassandra.NewJobHistoryRepository(session)
	archiverService := service.NewArchiverService(jobRepository, jobHistoryRepository,
		config.ArchiveInterval, config.ArchiveBatchSize, config.SucceededRetention, config.DeadRetention)

	archiverErrors := make(chan error, 1)
	go func() {
		archiverErrors <- archiverService.StartArchiver(ctx)
	}()

	select {
	case err := <-archiverErrors:
		if err != nil {
			session.Close()
			db.Close()
			log.Fatalf("archiver service: %v", err)
		}
	case <-ctx.Done():
		// A batch in progress gets the grace period to finish before the connections close.
		select {
		case <-archiverErrors:
		case <-time.After(config.ShutdownGracePeriod):
			log.Printf("archiver did not stop within %s", config.ShutdownGracePeriod)
		}
	}

	session.Close()
	if err := db.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
}
