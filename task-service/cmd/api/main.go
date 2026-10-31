package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/adapters/in/httpapi"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/adapters/out/cron"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/config"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/service"
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

	taskRepository := mysql.NewTaskRepository(db)
	taskService := service.NewTaskService(taskRepository, cron.NewCalculator(), time.Now)

	server := httpapi.NewServer(config, taskService)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Start()
	}()

	select {
	case err := <-serverErrors:
		db.Close()
		log.Fatalf("http server: %v", err)
	case <-ctx.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), config.ShutdownGracePeriod)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("shutdown http server: %v", err)
	}

	if err := db.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
}
