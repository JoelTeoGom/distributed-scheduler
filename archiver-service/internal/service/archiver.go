package service

import (
	"context"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/ports"
)

type ArchiverService struct {
	jobs               ports.JobRepository
	history            ports.JobHistoryRepository
	interval           time.Duration
	batchSize          int
	succeededRetention time.Duration
	deadRetention      time.Duration
}

var _ ports.ArchiverService = (*ArchiverService)(nil)

func NewArchiverService(jobs ports.JobRepository, history ports.JobHistoryRepository, interval time.Duration, batchSize int, succeededRetention, deadRetention time.Duration) *ArchiverService {
	return &ArchiverService{
		jobs:               jobs,
		history:            history,
		interval:           interval,
		batchSize:          batchSize,
		succeededRetention: succeededRetention,
		deadRetention:      deadRetention,
	}
}

func (s *ArchiverService) StartArchiver(ctx context.Context) error {
	return nil
}
