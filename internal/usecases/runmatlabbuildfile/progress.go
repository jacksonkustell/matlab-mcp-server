// Copyright 2026 The MathWorks, Inc.

package runmatlabbuildfile

import (
	"context"
	"fmt"
	"time"
)

const (
	heartbeatCount    = 3
	heartbeatInterval = 5 * time.Second
	heartbeatMessage  = "MATLAB build is still running"
)

// BuildProgressUpdate is a transport-neutral progress event for a MATLAB build.
type BuildProgressUpdate struct {
	Message  string
	Progress float64
	Total    float64
}

// ProgressUpdateSource obtains the next available MATLAB build progress event.
type ProgressUpdateSource interface {
	NextProgressUpdate(ctx context.Context) (BuildProgressUpdate, error)
}

// ProgressReporter sends a build progress event to a caller-specific destination.
type ProgressReporter interface {
	ReportProgress(ctx context.Context, update BuildProgressUpdate) error
}

// SyntheticHeartbeatProgressUpdateSource supplies placeholder progress events until MATLAB can supply them.
type SyntheticHeartbeatProgressUpdateSource struct{}

// NewSyntheticHeartbeatProgressUpdateSource creates a source for prototype build heartbeats.
func NewSyntheticHeartbeatProgressUpdateSource() *SyntheticHeartbeatProgressUpdateSource {
	return &SyntheticHeartbeatProgressUpdateSource{}
}

// NextProgressUpdate returns a synthetic heartbeat while MATLAB-backed progress is unavailable.
func (*SyntheticHeartbeatProgressUpdateSource) NextProgressUpdate(context.Context) (BuildProgressUpdate, error) {
	return BuildProgressUpdate{
		Message: heartbeatMessage,
	}, nil
}

// ProgressMonitor coordinates build progress retrieval and reporting.
type ProgressMonitor struct {
	source            ProgressUpdateSource
	heartbeatInterval time.Duration
}

// NewProgressMonitor creates a monitor that sends three heartbeat updates at five-second intervals.
func NewProgressMonitor(source ProgressUpdateSource) *ProgressMonitor {
	return &ProgressMonitor{
		source:            source,
		heartbeatInterval: heartbeatInterval,
	}
}

// Monitor reports the configured heartbeat updates until it completes, is canceled, or encounters an error.
func (m *ProgressMonitor) Monitor(ctx context.Context, reporter ProgressReporter) error {
	for progress := 1; progress <= heartbeatCount; progress++ {
		if err := waitForHeartbeatInterval(ctx, m.heartbeatInterval); err != nil {
			return err
		}

		update, err := m.source.NextProgressUpdate(ctx)
		if err != nil {
			return fmt.Errorf("get build progress update: %w", err)
		}

		update.Progress = float64(progress)
		update.Total = heartbeatCount
		if err := reporter.ReportProgress(ctx, update); err != nil {
			return fmt.Errorf("report build progress update: %w", err)
		}
	}

	return nil
}

func waitForHeartbeatInterval(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
