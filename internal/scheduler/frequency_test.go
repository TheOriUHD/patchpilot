package scheduler

import (
	"testing"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

func TestNextRuns(t *testing.T) {
	t.Parallel()
	from := time.Date(2025, 1, 5, 10, 15, 0, 0, time.UTC) // Sunday
	tests := []struct {
		name  string
		input core.ScheduleInput
		want  []time.Time
	}{
		{name: "hourly", input: core.ScheduleInput{Name: "h", Frequency: core.FrequencyHourly, Minute: 30}, want: []time.Time{
			time.Date(2025, 1, 5, 10, 30, 0, 0, time.UTC),
			time.Date(2025, 1, 5, 11, 30, 0, 0, time.UTC),
			time.Date(2025, 1, 5, 12, 30, 0, 0, time.UTC),
		}},
		{name: "daily", input: core.ScheduleInput{Name: "d", Frequency: core.FrequencyDaily, Minute: 5, Hour: 3}, want: []time.Time{
			time.Date(2025, 1, 6, 3, 5, 0, 0, time.UTC),
			time.Date(2025, 1, 7, 3, 5, 0, 0, time.UTC),
			time.Date(2025, 1, 8, 3, 5, 0, 0, time.UTC),
		}},
		{name: "weekly", input: core.ScheduleInput{Name: "w", Frequency: core.FrequencyWeekly, Minute: 0, Hour: 4, DayOfWeek: 1}, want: []time.Time{
			time.Date(2025, 1, 6, 4, 0, 0, 0, time.UTC),
			time.Date(2025, 1, 13, 4, 0, 0, 0, time.UTC),
			time.Date(2025, 1, 20, 4, 0, 0, 0, time.UTC),
		}},
		{name: "monthly", input: core.ScheduleInput{Name: "m", Frequency: core.FrequencyMonthly, Minute: 45, Hour: 2, DayOfMonth: 10}, want: []time.Time{
			time.Date(2025, 1, 10, 2, 45, 0, 0, time.UTC),
			time.Date(2025, 2, 10, 2, 45, 0, 0, time.UTC),
			time.Date(2025, 3, 10, 2, 45, 0, 0, time.UTC),
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NextRuns(test.input, from, 3)
			if err != nil {
				t.Fatalf("NextRuns() error: %v", err)
			}
			for index := range test.want {
				if !got[index].Equal(test.want[index]) {
					t.Fatalf("run %d = %s, want %s", index, got[index], test.want[index])
				}
			}
		})
	}
}

func TestCronExpression(t *testing.T) {
	t.Parallel()
	got, err := CronExpression(core.ScheduleInput{Name: "weekly", Frequency: core.FrequencyWeekly, Minute: 15, Hour: 2, DayOfWeek: 6})
	if err != nil {
		t.Fatalf("CronExpression() error: %v", err)
	}
	if got != "15 2 * * 6" {
		t.Fatalf("CronExpression() = %q", got)
	}
}
