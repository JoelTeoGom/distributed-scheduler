package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/config"
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

	<-ctx.Done()

	if err := db.Close(); err != nil {
		log.Printf("close database: %v", err)
	}

}
