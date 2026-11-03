package cassandra

import (
	"context"

	gocql "github.com/apache/cassandra-gocql-driver/v2"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/ports"
)

type JobHistoryRepository struct {
	session *gocql.Session
}

var _ ports.JobHistoryRepository = (*JobHistoryRepository)(nil)

func NewJobHistoryRepository(session *gocql.Session) *JobHistoryRepository {
	return &JobHistoryRepository{session: session}
}

func (r *JobHistoryRepository) CreateJob(ctx context.Context, job *domain.Job) error {
	return nil
}
