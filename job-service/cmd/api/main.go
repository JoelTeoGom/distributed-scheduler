package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/adapters/in/httpapi"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/adapters/out/cassandra"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/adapters/out/token"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/config"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/service"
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
	jobHistoryRepository := cassandra.NewJobHistoryRepository(session, time.Now)
	jobService := service.NewJobService(jobRepository, jobHistoryRepository, time.Now)
	tokens := token.NewJWT(config.JWTSecret)

	server := httpapi.NewServer(httpapi.Options{
		Address:        config.ServerAddress,
		ServiceName:    config.ServiceName,
		ServiceVersion: config.ServiceVersion,
	}, jobService, tokens)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Start()
	}()

	select {
	case err := <-serverErrors:
		session.Close()
		db.Close()
		log.Fatalf("http server: %v", err)
	case <-ctx.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), config.ShutdownGracePeriod)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("shutdown http server: %v", err)
	}

	session.Close()
	if err := db.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
}
