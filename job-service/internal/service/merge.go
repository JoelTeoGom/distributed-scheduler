package service

import (
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
)

// Both lists are sorted by ScheduledAt descending, and concatenating them would break that:
// an old dead job stays in MySQL while newer finished jobs are already in Cassandra.
func mergeHistory(hotJobs, archivedJobs []*domain.Job, limit int) []*domain.Job {
	merged := make([]*domain.Job, 0, limit)
	seen := make(map[uuid.UUID]bool, limit)
	hotIndex, archivedIndex := 0, 0

	for len(merged) < limit && (hotIndex < len(hotJobs) || archivedIndex < len(archivedJobs)) {
		var job *domain.Job
		if takeHot(hotJobs, archivedJobs, hotIndex, archivedIndex) {
			job = hotJobs[hotIndex]
			hotIndex++
		} else {
			job = archivedJobs[archivedIndex]
			archivedIndex++
		}

		if !seen[job.ID] {
			seen[job.ID] = true
			merged = append(merged, job)
		}
	}
	return merged
}

// On a tie the hot job goes first, so the MySQL copy of a duplicate is the one kept.
func takeHot(hotJobs, archivedJobs []*domain.Job, hotIndex, archivedIndex int) bool {
	if hotIndex == len(hotJobs) {
		return false
	}
	if archivedIndex == len(archivedJobs) {
		return true
	}
	return !hotJobs[hotIndex].ScheduledAt.Before(archivedJobs[archivedIndex].ScheduledAt)
}
