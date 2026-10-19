package config

import "time"

// Triggers (#34); docs/api-contract-m6.md describes them. The hook limits are in hooks.go.
const (
	// MinScheduleInterval is the shortest time between two firings of one schedule.
	MinScheduleInterval = 5 * time.Minute
	// TriggerBatch bounds the triggers or firings one pass of the worker takes.
	TriggerBatch = 20
	// TriggerFireLease is how long a worker holds a firing it is starting; one that dies delays it this long at most.
	TriggerFireLease = time.Minute
	// TriggerFireRetryFor is how long a firing that failed for a passing reason, such as a tool not answering, is
	// tried again before it is recorded as failed.
	TriggerFireRetryFor = 15 * time.Minute
	// TriggerFireRetention is how long ended firings are kept.
	TriggerFireRetention = 30 * 24 * time.Hour
	// TriggerSweepEvery is how often the worker looks for triggers whose owner's consent ended.
	TriggerSweepEvery = time.Minute
	// SchedulerLockKey is the advisory lock that lets one scheduler at a time take the due triggers.
	SchedulerLockKey = 0x726f746f72_0601
	// SchedulePreviewRuns is how many firings a schedule's preview shows.
	SchedulePreviewRuns = 5
	// MaxTriggerName bounds a trigger's name, as the table does.
	MaxTriggerName = 200
	// TriggerListLimit bounds the triggers one list answers.
	TriggerListLimit = 200
)
