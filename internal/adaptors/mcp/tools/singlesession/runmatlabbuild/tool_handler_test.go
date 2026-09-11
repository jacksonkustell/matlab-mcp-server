// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild_test

import (
	"testing"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/basetool"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/singlesession/runmatlabbuild"
	"github.com/matlab/matlab-mcp-server/internal/testutils"
	runmatlabbuildfileusecase "github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
	basetoolsmocks "github.com/matlab/matlab-mcp-server/mocks/adaptors/mcp/tools/basetool"
	mocks "github.com/matlab/matlab-mcp-server/mocks/adaptors/mcp/tools/singlesession/runmatlabbuild"
	entitiesmocks "github.com/matlab/matlab-mcp-server/mocks/entities"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNew_HappyPath(t *testing.T) {
	// Arrange
	mockLoggerFactory := &basetoolsmocks.MockLoggerFactory{}
	defer mockLoggerFactory.AssertExpectations(t)

	mockTelemetryFactory := &basetoolsmocks.MockTelemetryFactory{}
	defer mockTelemetryFactory.AssertExpectations(t)

	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockProgressMonitor := &mocks.MockProgressMonitor{}
	defer mockProgressMonitor.AssertExpectations(t)

	// Act
	tool := runmatlabbuild.New(mockLoggerFactory, mockTelemetryFactory, mockUsecase, mockGlobalMATLAB, mockProgressMonitor)

	// Assert
	assert.NotNil(t, tool)
}

func TestTool_Handler_NoProgressTokenReturnsFallback(t *testing.T) {
	// Arrange
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockProgressMonitor := &mocks.MockProgressMonitor{}
	defer mockProgressMonitor.AssertExpectations(t)

	mockMATLABSessionClient := &entitiesmocks.MockMATLABSessionClient{}
	defer mockMATLABSessionClient.AssertExpectations(t)

	mockLogger := testutils.NewInspectableLogger()
	ctx := t.Context()
	const buildfilePath = "/project/buildfile.m"
	args := runmatlabbuild.Args{
		BuildfilePath: buildfilePath,
	}

	mockGlobalMATLAB.EXPECT().
		Client(ctx, mockLogger.AsMockArg()).
		Return(mockMATLABSessionClient, nil).
		Once()

	mockUsecase.EXPECT().
		Execute(ctx, mockLogger.AsMockArg(), mockMATLABSessionClient, runmatlabbuildfileusecase.Args{
			BuildfilePath: buildfilePath,
		}).
		Return(nil).
		Once()

	// Act
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB, mockProgressMonitor)(ctx, mockLogger, basetool.ToolCallRequest{}, args)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, runmatlabbuild.ReturnArgs{
		Message: "Build triggered successfully. No progress token was supplied, so no progress updates were sent.",
	}, result)
	mockProgressMonitor.AssertNotCalled(t, "Monitor", mock.Anything, mock.Anything)
}

func TestTool_Handler_ClientReturnsError(t *testing.T) {
	// Arrange
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockProgressMonitor := &mocks.MockProgressMonitor{}
	defer mockProgressMonitor.AssertExpectations(t)

	mockLogger := testutils.NewInspectableLogger()
	ctx := t.Context()
	expectedError := assert.AnError

	mockGlobalMATLAB.EXPECT().
		Client(ctx, mockLogger.AsMockArg()).
		Return(nil, expectedError).
		Once()

	// Act
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB, mockProgressMonitor)(ctx, mockLogger, basetool.ToolCallRequest{}, runmatlabbuild.Args{
		BuildfilePath: "/project/buildfile.m",
	})

	// Assert
	require.ErrorIs(t, err, expectedError)
	assert.Empty(t, result)
}

func TestTool_Handler_UsecaseReturnsValidationError(t *testing.T) {
	// Arrange
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockProgressMonitor := &mocks.MockProgressMonitor{}
	defer mockProgressMonitor.AssertExpectations(t)

	mockMATLABSessionClient := &entitiesmocks.MockMATLABSessionClient{}
	defer mockMATLABSessionClient.AssertExpectations(t)

	mockLogger := testutils.NewInspectableLogger()
	ctx := t.Context()
	const buildfilePath = "/invalid/buildfile.m"
	expectedError := assert.AnError

	mockGlobalMATLAB.EXPECT().
		Client(ctx, mockLogger.AsMockArg()).
		Return(mockMATLABSessionClient, nil).
		Once()

	mockUsecase.EXPECT().
		Execute(ctx, mockLogger.AsMockArg(), mockMATLABSessionClient, runmatlabbuildfileusecase.Args{
			BuildfilePath: buildfilePath,
		}).
		Return(expectedError).
		Once()

	// Act
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB, mockProgressMonitor)(ctx, mockLogger, basetool.ToolCallRequest{}, runmatlabbuild.Args{
		BuildfilePath: buildfilePath,
	})

	// Assert
	require.ErrorIs(t, err, expectedError)
	assert.Empty(t, result)
}

func TestRunMATLABBuild_Annotations(t *testing.T) {
	// Arrange
	mockLoggerFactory := &basetoolsmocks.MockLoggerFactory{}
	defer mockLoggerFactory.AssertExpectations(t)

	mockTelemetryFactory := &basetoolsmocks.MockTelemetryFactory{}
	defer mockTelemetryFactory.AssertExpectations(t)

	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockProgressMonitor := &mocks.MockProgressMonitor{}
	defer mockProgressMonitor.AssertExpectations(t)

	// Act
	tool := runmatlabbuild.New(mockLoggerFactory, mockTelemetryFactory, mockUsecase, mockGlobalMATLAB, mockProgressMonitor)

	// Assert
	assert.Equal(t, annotations.NewDestructiveAnnotations(), tool.Annotations())
}

func TestTool_Handler_ReportsProgressWhenClientSuppliesToken(t *testing.T) {
	// Arrange
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockProgressMonitor := &mocks.MockProgressMonitor{}
	defer mockProgressMonitor.AssertExpectations(t)

	mockMATLABSessionClient := &entitiesmocks.MockMATLABSessionClient{}
	defer mockMATLABSessionClient.AssertExpectations(t)

	mockLogger := testutils.NewInspectableLogger()
	ctx := t.Context()
	const buildfilePath = "/project/buildfile.m"
	const progressToken = "build-progress-token"
	session := &mcp.ServerSession{}

	mockGlobalMATLAB.EXPECT().
		Client(ctx, mockLogger.AsMockArg()).
		Return(mockMATLABSessionClient, nil).
		Once()
	mockUsecase.EXPECT().
		Execute(ctx, mockLogger.AsMockArg(), mockMATLABSessionClient, runmatlabbuildfileusecase.Args{
			BuildfilePath: buildfilePath,
		}).
		Return(nil).
		Once()
	mockProgressMonitor.EXPECT().
		Monitor(ctx, mock.AnythingOfType("*runmatlabbuild.MCPProgressReporter")).
		Return(nil).
		Once()

	// Act
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB, mockProgressMonitor)(ctx, mockLogger, basetool.ToolCallRequest{
		Session:       session,
		ProgressToken: progressToken,
	}, runmatlabbuild.Args{
		BuildfilePath: buildfilePath,
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, runmatlabbuild.ReturnArgs{
		Message:       "Build triggered successfully",
		ProgressToken: progressToken,
	}, result)
}

func TestTool_Handler_ReturnsProgressMonitorFailureAfterLaunchingBuild(t *testing.T) {
	// Arrange
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockProgressMonitor := &mocks.MockProgressMonitor{}
	defer mockProgressMonitor.AssertExpectations(t)

	mockMATLABSessionClient := &entitiesmocks.MockMATLABSessionClient{}
	defer mockMATLABSessionClient.AssertExpectations(t)

	mockLogger := testutils.NewInspectableLogger()
	ctx := t.Context()
	expectedError := assert.AnError

	mockGlobalMATLAB.EXPECT().
		Client(ctx, mockLogger.AsMockArg()).
		Return(mockMATLABSessionClient, nil).
		Once()
	mockUsecase.EXPECT().
		Execute(ctx, mockLogger.AsMockArg(), mockMATLABSessionClient, runmatlabbuildfileusecase.Args{
			BuildfilePath: "/project/buildfile.m",
		}).
		Return(nil).
		Once()
	mockProgressMonitor.EXPECT().
		Monitor(ctx, mock.AnythingOfType("*runmatlabbuild.MCPProgressReporter")).
		Return(expectedError).
		Once()

	// Act
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB, mockProgressMonitor)(ctx, mockLogger, basetool.ToolCallRequest{
		Session:       &mcp.ServerSession{},
		ProgressToken: "build-progress-token",
	}, runmatlabbuild.Args{
		BuildfilePath: "/project/buildfile.m",
	})

	// Assert
	require.NoError(t, err)
	assert.Contains(t, result.Message, "Build triggered successfully, but progress monitoring failed:")
	assert.Contains(t, result.Message, expectedError.Error())
	assert.Equal(t, "build-progress-token", result.ProgressToken)
}
