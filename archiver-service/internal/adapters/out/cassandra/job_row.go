package cassandra

import (
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// The partition holds one month of a task, e.g. 202610.
func bucketOf(scheduledAt time.Time) int {
	scheduledAt = scheduledAt.UTC()
	return scheduledAt.Year()*100 + int(scheduledAt.Month())
}

// Binding nil in an INSERT writes a tombstone in Cassandra. UnsetValue leaves the column out instead.

func httpStatusValue(httpStatus *int) any {
	if httpStatus == nil {
		return gocql.UnsetValue
	}
	return int16(*httpStatus)
}

func textValue(text string) any {
	if text == "" {
		return gocql.UnsetValue
	}
	return text
}

func bytesValue(data []byte) any {
	if len(data) == 0 {
		return gocql.UnsetValue
	}
	return data
}

func timeValue(moment *time.Time) any {
	if moment == nil {
		return gocql.UnsetValue
	}
	return *moment
}
