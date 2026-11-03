package cron_test

import (
	"errors"
	"testing"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/adapters/out/cron"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/domain"
)

func TestNextUsesTheTaskTimezone(t *testing.T) {
	after := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)

	next, err := cron.NewCalculator().Next("0 9 * * *", "Europe/Madrid", after)
	if err != nil {
		t.Fatal(err)
	}

	want := time.Date(2026, time.January, 16, 8, 0, 0, 0, time.UTC)
	if !next.Equal(want) || next.Location() != time.UTC {
		t.Errorf("got %v, want %v in UTC", next, want)
	}
}

func TestNextFollowsDaylightSaving(t *testing.T) {
	after := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

	next, err := cron.NewCalculator().Next("0 9 * * *", "Europe/Madrid", after)
	if err != nil {
		t.Fatal(err)
	}

	want := time.Date(2026, time.July, 16, 7, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("got %v, want %v", next, want)
	}
}

func TestNextRejectsInvalidSchedules(t *testing.T) {
	after := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)

	testCases := map[string]struct {
		cronExpr string
		timezone string
	}{
		"malformed expression": {"99 * * * *", "UTC"},
		"never fires":          {"0 0 31 2 *", "UTC"},
		"descriptor":           {"@hourly", "UTC"},
		"unknown timezone":     {"* * * * *", "Mars/Olympus"},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := cron.NewCalculator().Next(testCase.cronExpr, testCase.timezone, after)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("want ErrInvalidInput, got %v", err)
			}
		})
	}
}
