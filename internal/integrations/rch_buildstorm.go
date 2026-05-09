package integrations

import (
	"context"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/pressure"
	"github.com/Dicklesworthstone/ntm/internal/tools"
)

// RCHBuildStormReader is the small part of tools.RCHAdapter needed by the
// build-storm coordinator. Tests can provide a deterministic double.
type RCHBuildStormReader interface {
	GetAvailability(context.Context) (*tools.RCHAvailability, error)
	GetStatus(context.Context) (*tools.RCHStatus, error)
}

// RCHBuildStormOptions adds local scheduler/process context to rch status.
type RCHBuildStormOptions struct {
	Now                  time.Time
	Session              string
	RequestedBuilds      int
	LocalBuildCount      int
	MaxLocalBuilds       int
	SessionRunningBuilds int
	MaxBuildsPerSession  int
}

// EvaluateRCHBuildStorm reads rch availability/status and returns an advisory
// build-storm report. It does not acquire slots or mutate scheduler state.
func EvaluateRCHBuildStorm(ctx context.Context, reader RCHBuildStormReader, opts RCHBuildStormOptions) (pressure.BuildStormReport, error) {
	if reader == nil {
		return pressure.EvaluateBuildStorm(BuildStormInputFromRCHStatus(nil, nil, opts)), nil
	}

	availability, err := reader.GetAvailability(ctx)
	if err != nil {
		return pressure.BuildStormReport{}, err
	}
	var status *tools.RCHStatus
	if availability != nil && availability.Available && availability.Compatible {
		status, err = reader.GetStatus(ctx)
		if err != nil {
			return pressure.BuildStormReport{}, err
		}
	}
	return pressure.EvaluateBuildStorm(BuildStormInputFromRCHStatus(status, availability, opts)), nil
}

// BuildStormInputFromRCHStatus maps tools.RCHStatus into the pressure package's
// pure decision input.
func BuildStormInputFromRCHStatus(status *tools.RCHStatus, availability *tools.RCHAvailability, opts RCHBuildStormOptions) pressure.BuildStormInput {
	in := pressure.BuildStormInput{
		Now:                  opts.Now,
		Session:              opts.Session,
		RequestedBuilds:      opts.RequestedBuilds,
		LocalBuildCount:      opts.LocalBuildCount,
		MaxLocalBuilds:       opts.MaxLocalBuilds,
		SessionRunningBuilds: opts.SessionRunningBuilds,
		MaxBuildsPerSession:  opts.MaxBuildsPerSession,
	}
	if availability != nil {
		in.RCHAvailable = availability.Available
		in.RCHCompatible = availability.Compatible
		in.WorkerCount = availability.WorkerCount
		in.HealthyWorkers = availability.HealthyCount
	}
	if status == nil {
		return in
	}
	in.RCHAvailable = status.Enabled || in.RCHAvailable
	if availability == nil {
		in.RCHCompatible = in.RCHAvailable
	}
	if status.WorkerCount > 0 {
		in.WorkerCount = status.WorkerCount
	} else if len(status.Workers) > 0 {
		// When aggregate counts are absent, prefer the current workers payload
		// over cached availability snapshots.
		in.WorkerCount = len(status.Workers)
	}
	if len(status.Workers) > in.WorkerCount {
		in.WorkerCount = len(status.Workers)
	}
	if status.HealthyCount > 0 {
		in.HealthyWorkers = status.HealthyCount
	} else if len(status.Workers) > 0 {
		// status healthy_count may be omitted in some rch JSON variants. Avoid
		// retaining stale cached health counts in that case.
		in.HealthyWorkers = 0
	}

	busy := 0
	healthyFromWorkers := 0
	queueDepth := 0
	for _, worker := range status.Workers {
		if worker.Queue > 0 {
			queueDepth += worker.Queue
		}
		if !worker.Available || !worker.Healthy {
			continue
		}
		healthyFromWorkers++
		if rchWorkerBusy(worker) {
			busy++
		}
	}
	if in.HealthyWorkers == 0 {
		in.HealthyWorkers = healthyFromWorkers
	}
	in.BusyWorkers = busy
	in.QueueDepth = queueDepth
	return in
}

func rchWorkerBusy(worker tools.RCHWorker) bool {
	if worker.Queue > 0 || worker.Load >= 80 || worker.CPUPercent >= 90 {
		return true
	}
	return strings.TrimSpace(worker.CurrentBuild) != ""
}
