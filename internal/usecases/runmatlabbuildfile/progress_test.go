// Copyright 2026 The MathWorks, Inc.

package runmatlabbuildfile_test

import (
	"context"
	"testing"
	"time"

	"github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
	mocks "github.com/matlab/matlab-mcp-server/mocks/usecases/runmatlabbuildfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyntheticHeartbeatProgressUpdateSource_NextProgressUpdate_ReturnsHeartbeat(t *testing.T) {
	// Arrange
	source := runmatlabbuildfile.NewSyntheticHeartbeatProgressUpdateSource()

	// Act
	update, err := source.NextProgressUpdate(t.Context())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, runmatlabbuildfile.BuildProgressUpdate{
		Message: "MATLAB build is still running",
	}, update)
}

func TestProgressMonitor_Monitor_ReportsThreeHeartbeatsWithZeroDelay(t *testing.T) {
	// Arrange
	ctx := t.Context()
	source := runmatlabbuildfile.NewSyntheticHeartbeatProgressUpdateSource()
	monitor := runmatlabbuildfile.NewProgressMonitor(source)
	monitor.SetHeartbeatInterval(0)

	reporter := &mocks.MockProgressReporter{}
	defer reporter.AssertExpectations(t)

	expectedUpdates := []runmatlabbuildfile.BuildProgressUpdate{
		{Message: "MATLAB build is still running", Progress: 1, Total: 3},
		{Message: "MATLAB build is still running", Progress: 2, Total: 3},
		{Message: "MATLAB build is still running", Progress: 3, Total: 3},
	}
	for _, expectedUpdate := range expectedUpdates {
		reporter.EXPECT().
			ReportProgress(ctx, expectedUpdate).
			Return(nil).
			Once()
	}

	startTime := time.Now()

	// Act
	err := monitor.Monitor(ctx, reporter)

	// Assert
	require.NoError(t, err)
	assert.Less(t, time.Since(startTime), time.Second)
}

func TestProgressMonitor_Monitor_SourceFailureStopsMonitoring(t *testing.T) {
	// Arrange
	ctx := t.Context()
	expectedError := assert.AnError

	source := &mocks.MockProgressUpdateSource{}
	defer source.AssertExpectations(t)
	source.EXPECT().
		NextProgressUpdate(ctx).
		Return(runmatlabbuildfile.BuildProgressUpdate{}, expectedError).
		Once()

	reporter := &mocks.MockProgressReporter{}
	defer reporter.AssertExpectations(t)

	monitor := runmatlabbuildfile.NewProgressMonitor(source)
	monitor.SetHeartbeatInterval(0)

	// Act
	err := monitor.Monitor(ctx, reporter)

	// Assert
	require.ErrorIs(t, err, expectedError)
}

func TestProgressMonitor_Monitor_ReporterFailureStopsMonitoring(t *testing.T) {
	// Arrange
	ctx := t.Context()
	expectedError := assert.AnError
	sourceUpdate := runmatlabbuildfile.BuildProgressUpdate{
		Message: "Build heartbeat",
	}
	expectedReportedUpdate := runmatlabbuildfile.BuildProgressUpdate{
		Message:  "Build heartbeat",
		Progress: 1,
		Total:    3,
	}

	source := &mocks.MockProgressUpdateSource{}
	defer source.AssertExpectations(t)
	source.EXPECT().
		NextProgressUpdate(ctx).
		Return(sourceUpdate, nil).
		Once()

	reporter := &mocks.MockProgressReporter{}
	defer reporter.AssertExpectations(t)
	reporter.EXPECT().
		ReportProgress(ctx, expectedReportedUpdate).
		Return(expectedError).
		Once()

	monitor := runmatlabbuildfile.NewProgressMonitor(source)
	monitor.SetHeartbeatInterval(0)

	// Act
	err := monitor.Monitor(ctx, reporter)

	// Assert
	require.ErrorIs(t, err, expectedError)
}

func TestProgressMonitor_Monitor_CancellationStopsBeforeFirstUpdate(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	source := &mocks.MockProgressUpdateSource{}
	defer source.AssertExpectations(t)

	reporter := &mocks.MockProgressReporter{}
	defer reporter.AssertExpectations(t)

	monitor := runmatlabbuildfile.NewProgressMonitor(source)

	// Act
	err := monitor.Monitor(ctx, reporter)

	// Assert
	require.ErrorIs(t, err, context.Canceled)
}
