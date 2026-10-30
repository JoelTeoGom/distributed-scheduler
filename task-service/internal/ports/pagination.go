package ports

import "github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"

type PageRequest struct {
	Cursor string
	Limit  int
}

type TaskPage struct {
	Tasks      []domain.Task
	NextCursor string
}
