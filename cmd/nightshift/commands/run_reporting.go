package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/logging"
	"github.com/marcus/nightshift/internal/reporting"
)

type runReport struct {
	results    *reporting.RunResults
	usedBudget int
}

func newRunReport(start time.Time) *runReport {
	return &runReport{
		results: &reporting.RunResults{
			Date:      start,
			StartTime: start,
			UsedBudget: 0,
			Tasks:     []reporting.TaskResult{},
		},
	}
}

func (r *runReport) addTask(task reporting.TaskResult) {
	r.results.Tasks = append(r.results.Tasks, task)
	r.usedBudget += task.TokensUsed
}

func (r *runReport) finalize(cfg *config.Config, log *logging.Logger) {
	r.finalizeWithDB(cfg, log, nil)
}

func (r *runReport) finalizeWithDB(cfg *config.Config, log *logging.Logger, database *db.DB) {
	if r == nil || r.results == nil || cfg == nil {
		return
	}

	r.results.EndTime = time.Now()
	r.results.UsedBudget = r.usedBudget

	logPath := ""
	if cfg.ExpandedLogPath() != "" {
		logPath = filepath.Join(cfg.ExpandedLogPath(), fmt.Sprintf("nightshift-%s.log", r.results.StartTime.Format("2006-01-02")))
	}
	r.results.LogPath = logPath

	// Fetch cost snapshots via active tracking.
	{
		ctx := context.Background()
		snapshots := budget.FetchAllCostSnapshots(ctx)
		r.results.CostSnapshots = snapshots
		// Persist for yesterday-delta in future summaries.
		if database != nil {
			for i := range snapshots {
				if err := database.SaveCostSnapshot(ctx, &snapshots[i]); err != nil {
					log.Warnf("cost snapshot save (%s): %v", snapshots[i].Provider, err)
				}
			}
		}
	}

	if cfg.Reporting.MorningSummary {
		gen := reporting.NewGenerator(cfg)
		if database != nil {
			gen = gen.WithCostHistory(database)
		}
		summary, err := gen.Generate(r.results)
		if err != nil {
			log.Warnf("summary generate: %v", err)
		} else {
			path := reporting.DefaultSummaryPath(r.results.Date)
			if err := gen.Save(summary, path); err != nil {
				log.Warnf("summary save: %v", err)
			} else {
				log.Infof("summary saved: %s", path)
			}
		}
	}

	reportPath := reporting.DefaultRunReportPath(r.results.EndTime)
	if err := reporting.SaveRunReport(r.results, reportPath, r.results.LogPath); err != nil {
		log.Warnf("run report save: %v", err)
	} else {
		log.Infof("run report saved: %s", reportPath)
	}

	resultsPath := reporting.DefaultRunResultsPath(r.results.EndTime)
	if err := reporting.SaveRunResults(r.results, resultsPath); err != nil {
		log.Warnf("run results save: %v", err)
	} else {
		log.Infof("run results saved: %s", resultsPath)
	}
}
