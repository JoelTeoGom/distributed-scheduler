package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/adapter/config"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/adapter/httpserver"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	config, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	var taskService ports.TaskService

	server := httpserver.NewServer(config, taskService)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Start()
	}()

	select {
	case err := <-serverErrors:
		log.Fatalf("http server: %v", err)
	case <-ctx.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), config.ShutdownGracePeriod)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("shutdown http server: %v", err)
	}
}
