// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/singlesession/runmatlabbuild"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/telemetry"
	"github.com/matlab/matlab-mcp-server/internal/testutils"
	"github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
	basetoolmocks "github.com/matlab/matlab-mcp-server/mocks/adaptors/mcp/tools/basetool"
	mocks "github.com/matlab/matlab-mcp-server/mocks/adaptors/mcp/tools/singlesession/runmatlabbuild"
	telemetrymocks "github.com/matlab/matlab-mcp-server/mocks/adaptors/telemetry"
	entitiesmocks "github.com/matlab/matlab-mcp-server/mocks/entities"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMCPProgressReporter_ReportProgress_MapsBuildProgressUpdate(t *testing.T) {
	// Arrange
	ctx := t.Context()
	const progressToken = "build-progress-token"
	update := runmatlabbuildfile.BuildProgressUpdate{
		Message:  "MATLAB build is still running",
		Progress: 2,
		Total:    3,
	}

	sender := &mocks.MockProgressNotificationSender{}
	defer sender.AssertExpectations(t)
	sender.EXPECT().
		NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			Message:       update.Message,
			ProgressToken: progressToken,
			Progress:      update.Progress,
			Total:         update.Total,
		}).
		Return(nil).
		Once()

	reporter := runmatlabbuild.NewMCPProgressReporter(sender, progressToken)

	// Act
	err := reporter.ReportProgress(ctx, update)

	// Assert
	require.NoError(t, err)
}

func TestRunMATLABBuild_ProgressNotificationsReachClientWithSuppliedToken(t *testing.T) {
	// Arrange
	ctx := t.Context()
	const (
		buildfilePath = "/project/buildfile.m"
		progressToken = "build-progress-token"
	)
	expectedUpdates := []runmatlabbuildfile.BuildProgressUpdate{
		{Message: "MATLAB build is still running", Progress: 1, Total: 3},
		{Message: "MATLAB build is still running", Progress: 2, Total: 3},
		{Message: "MATLAB build is still running", Progress: 3, Total: 3},
	}

	mockLoggerFactory := &basetoolmocks.MockLoggerFactory{}
	defer mockLoggerFactory.AssertExpectations(t)
	mockTelemetryFactory := &basetoolmocks.MockTelemetryFactory{}
	defer mockTelemetryFactory.AssertExpectations(t)
	mockTelemetry := &telemetrymocks.MockTelemetry{}
	defer mockTelemetry.AssertExpectations(t)
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)
	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)
	mockMATLABSessionClient := &entitiesmocks.MockMATLABSessionClient{}
	defer mockMATLABSessionClient.AssertExpectations(t)
	logger := testutils.NewInspectableLogger()

	mockLoggerFactory.EXPECT().
		NewMCPSessionLogger(mock.Anything).
		Return(logger, nil).
		Once()
	mockTelemetryFactory.EXPECT().
		Telemetry().
		Return(mockTelemetry, nil).
		Once()
	mockTelemetry.EXPECT().
		RecordToolCallRequest(mock.Anything, "run_matlab_build", telemetry.ToolSourceBuiltin).
		Return().
		Once()
	mockGlobalMATLAB.EXPECT().
		Client(mock.Anything, mock.Anything).
		Return(mockMATLABSessionClient, nil).
		Once()
	mockUsecase.EXPECT().
		Execute(mock.Anything, mock.Anything, mockMATLABSessionClient, runmatlabbuildfile.Args{
			BuildfilePath: buildfilePath,
		}).
		Return(nil).
		Once()

	server := mcp.NewServer(&mcp.Implementation{Name: "progress-test-server", Version: "1.0.0"}, nil)
	tool := runmatlabbuild.New(
		mockLoggerFactory,
		mockTelemetryFactory,
		mockUsecase,
		mockGlobalMATLAB,
		immediateProgressMonitor{updates: expectedUpdates},
	)
	require.NoError(t, tool.AddToServer(server))

	progressNotifications := make(chan mcp.ProgressNotificationParams, len(expectedUpdates))
	client := mcp.NewClient(&mcp.Implementation{Name: "progress-test-client", Version: "1.0.0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, request *mcp.ProgressNotificationClientRequest) {
			progressNotifications <- *request.Params
		},
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})

	// Act
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "run_matlab_build",
		Arguments: map[string]any{
			"buildfile_path": buildfilePath,
		},
		Meta: mcp.Meta{
			"progressToken": progressToken,
		},
	})

	// Assert
	require.NoError(t, err)
	var output runmatlabbuild.ReturnArgs
	structuredContent, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(structuredContent, &output))
	assert.Equal(t, runmatlabbuild.ReturnArgs{
		Message:       "Build triggered successfully",
		ProgressToken: progressToken,
	}, output)

	for _, expectedUpdate := range expectedUpdates {
		select {
		case notification := <-progressNotifications:
			assert.Equal(t, progressToken, notification.ProgressToken)
			assert.Equal(t, expectedUpdate.Message, notification.Message)
			assert.Equal(t, expectedUpdate.Progress, notification.Progress)
			assert.Equal(t, expectedUpdate.Total, notification.Total)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for MCP progress notification")
		}
	}
}

type immediateProgressMonitor struct {
	updates []runmatlabbuildfile.BuildProgressUpdate
}

func (m immediateProgressMonitor) Monitor(ctx context.Context, reporter runmatlabbuildfile.ProgressReporter) error {
	for _, update := range m.updates {
		if err := reporter.ReportProgress(ctx, update); err != nil {
			return err
		}
	}

	return nil
}
