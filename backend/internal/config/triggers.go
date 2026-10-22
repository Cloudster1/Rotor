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

// The write ledger (#127); docs/api-contract-m12.md describes it.
const (
	// MaxWriteSubjects bounds what one tool call records it wrote.
	MaxWriteSubjects = 20
	// MaxWriteSubject bounds one recorded subject, as the table does.
	MaxWriteSubject = 500
	// OwnWriteWindow is how far apart a write and an event about it may be to count as the same: an issue key names
	// every later change too, which a person's edit an hour on must not be mistaken for.
	OwnWriteWindow = 10 * time.Minute
)
