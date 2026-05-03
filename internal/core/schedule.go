package core

import "time"

// Frequency identifies how often a schedule fires.
type Frequency string

const (
	// FrequencyHourly fires once per hour.
	FrequencyHourly Frequency = "hourly"
	// FrequencyDaily fires once per day.
	FrequencyDaily Frequency = "daily"
	// FrequencyWeekly fires once per week.
	FrequencyWeekly Frequency = "weekly"
	// FrequencyMonthly fires once per month.
	FrequencyMonthly Frequency = "monthly"
)

// Schedule describes an update schedule and its assigned hosts.
type Schedule struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Frequency      Frequency  `json:"frequency"`
	Minute         int        `json:"minute"`
	Hour           int        `json:"hour"`
	DayOfWeek      int        `json:"day_of_week"`
	DayOfMonth     int        `json:"day_of_month"`
	Enabled        bool       `json:"enabled"`
	MaxConcurrent  int        `json:"max_concurrent"`
	HostIDs        []int64    `json:"host_ids"`
	HostCount      int        `json:"host_count"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	LastSkippedAt  *time.Time `json:"last_skipped_at,omitempty"`
	LastSkipReason string     `json:"last_skip_reason"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ScheduleInput describes fields used to create or update a schedule.
type ScheduleInput struct {
	Name          string    `json:"name"`
	Frequency     Frequency `json:"frequency"`
	Minute        int       `json:"minute"`
	Hour          int       `json:"hour"`
	DayOfWeek     int       `json:"day_of_week"`
	DayOfMonth    int       `json:"day_of_month"`
	Enabled       bool      `json:"enabled"`
	MaxConcurrent int       `json:"max_concurrent"`
	HostIDs       []int64   `json:"host_ids"`
}
