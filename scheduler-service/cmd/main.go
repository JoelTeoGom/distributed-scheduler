package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os/signal"
	"syscall"

	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/adapters/out/cron"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/config"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/service"
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

	if config.PprofAddr != "" {
		go func() {
			log.Printf("pprof listening on %s", config.PprofAddr)
			log.Printf("pprof: %v", http.ListenAndServe(config.PprofAddr, nil))
		}()
	}

	schedulerRepository := mysql.NewSchedulerRepository(db, config.ScheduleLookahead)
	cron := cron.NewCalculator()

	schedulerService := service.NewSchedulerService(schedulerRepository, cron, config.ScheduleInterval)

	schedulerErrors := make(chan error, 1)
	go func() {
		schedulerErrors <- schedulerService.Start(ctx)
	}()

	select {
	case err := <-schedulerErrors:
		db.Close()
		log.Fatalf("scheduler service: %v", err)
	case <-ctx.Done():
	}

	if err := db.Close(); err != nil {
		log.Printf("close database: %v", err)
	}

}
