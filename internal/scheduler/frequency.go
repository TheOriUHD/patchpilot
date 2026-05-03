package scheduler

import (
	"fmt"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// InputFromSchedule converts a stored schedule to editable schedule input.
func InputFromSchedule(schedule core.Schedule) core.ScheduleInput {
	return core.ScheduleInput{Name: schedule.Name, Frequency: schedule.Frequency, Minute: schedule.Minute, Hour: schedule.Hour, DayOfWeek: schedule.DayOfWeek, DayOfMonth: schedule.DayOfMonth, Enabled: schedule.Enabled, MaxConcurrent: schedule.MaxConcurrent, HostIDs: append([]int64(nil), schedule.HostIDs...)}
}

// Validate checks schedule timing fields.
func Validate(input core.ScheduleInput) error {
	if input.Name == "" {
		return fmt.Errorf("schedule name is required")
	}
	if input.Minute < 0 || input.Minute > 59 {
		return fmt.Errorf("minute must be between 0 and 59")
	}
	switch input.Frequency {
	case core.FrequencyHourly:
		return nil
	case core.FrequencyDaily:
		return validateHour(input.Hour)
	case core.FrequencyWeekly:
		if err := validateHour(input.Hour); err != nil {
			return err
		}
		if input.DayOfWeek < 0 || input.DayOfWeek > 6 {
			return fmt.Errorf("day_of_week must be between 0 and 6")
		}
		return nil
	case core.FrequencyMonthly:
		if err := validateHour(input.Hour); err != nil {
			return err
		}
		if input.DayOfMonth < 1 || input.DayOfMonth > 28 {
			return fmt.Errorf("day_of_month must be between 1 and 28")
		}
		return nil
	default:
		return fmt.Errorf("unsupported frequency %q", input.Frequency)
	}
}

// CronExpression returns the five-field cron expression used by gocron.
func CronExpression(input core.ScheduleInput) (string, error) {
	if err := Validate(input); err != nil {
		return "", err
	}
	switch input.Frequency {
	case core.FrequencyHourly:
		return fmt.Sprintf("%d * * * *", input.Minute), nil
	case core.FrequencyDaily:
		return fmt.Sprintf("%d %d * * *", input.Minute, input.Hour), nil
	case core.FrequencyWeekly:
		return fmt.Sprintf("%d %d * * %d", input.Minute, input.Hour, input.DayOfWeek), nil
	case core.FrequencyMonthly:
		return fmt.Sprintf("%d %d %d * *", input.Minute, input.Hour, input.DayOfMonth), nil
	default:
		return "", fmt.Errorf("unsupported frequency %q", input.Frequency)
	}
}

// NextRuns returns the next count occurrences after from.
func NextRuns(input core.ScheduleInput, from time.Time, count int) ([]time.Time, error) {
	if err := Validate(input); err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, nil
	}
	runs := make([]time.Time, 0, count)
	cursor := from
	for len(runs) < count {
		next := nextRun(input, cursor)
		runs = append(runs, next)
		cursor = next
	}
	return runs, nil
}

func nextRun(input core.ScheduleInput, from time.Time) time.Time {
	loc := from.Location()
	switch input.Frequency {
	case core.FrequencyHourly:
		candidate := time.Date(from.Year(), from.Month(), from.Day(), from.Hour(), input.Minute, 0, 0, loc)
		if !candidate.After(from) {
			candidate = candidate.Add(time.Hour)
		}
		return candidate
	case core.FrequencyDaily:
		candidate := time.Date(from.Year(), from.Month(), from.Day(), input.Hour, input.Minute, 0, 0, loc)
		if !candidate.After(from) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate
	case core.FrequencyWeekly:
		days := (int(time.Weekday(input.DayOfWeek)) - int(from.Weekday()) + 7) % 7
		candidate := time.Date(from.Year(), from.Month(), from.Day(), input.Hour, input.Minute, 0, 0, loc).AddDate(0, 0, days)
		if !candidate.After(from) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate
	case core.FrequencyMonthly:
		candidate := time.Date(from.Year(), from.Month(), input.DayOfMonth, input.Hour, input.Minute, 0, 0, loc)
		if !candidate.After(from) {
			candidate = candidate.AddDate(0, 1, 0)
		}
		return candidate
	default:
		return from
	}
}

func validateHour(hour int) error {
	if hour < 0 || hour > 23 {
		return fmt.Errorf("hour must be between 0 and 23")
	}
	return nil
}
