package commands

import (
	"context"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/db"
	"github.com/cedricfarinazzo/nightshift/internal/logging"
)

// schedulerFailureSink satisfies scheduler.FailureSink.
// It logs the error and persists it to the DB so status/doctor can surface it.
type schedulerFailureSink struct {
	database *db.DB
	log      *logging.Logger
}

func (s *schedulerFailureSink) RecordSchedulerFailure(ctx context.Context, jobName string, failedAt time.Time, errText string) {
	s.log.Errorf("scheduler: job %s failed: %s", jobName, errText)
	if err := db.RecordSchedulerFailure(ctx, s.database, jobName, failedAt, errText); err != nil {
		s.log.Errorf("scheduler: failed to persist failure for job %s: %v", jobName, err)
	}
}
