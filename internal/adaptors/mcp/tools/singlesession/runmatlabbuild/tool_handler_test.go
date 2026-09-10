// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild_test

import (
	"testing"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/singlesession/runmatlabbuild"
	"github.com/matlab/matlab-mcp-server/internal/testutils"
	runmatlabbuildfileusecase "github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
	basetoolsmocks "github.com/matlab/matlab-mcp-server/mocks/adaptors/mcp/tools/basetool"
	mocks "github.com/matlab/matlab-mcp-server/mocks/adaptors/mcp/tools/singlesession/runmatlabbuild"
	entitiesmocks "github.com/matlab/matlab-mcp-server/mocks/entities"
	"github.com/stretchr/testify/assert"
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

	// Act
	tool := runmatlabbuild.New(mockLoggerFactory, mockTelemetryFactory, mockUsecase, mockGlobalMATLAB)

	// Assert
	assert.NotNil(t, tool)
}

func TestTool_Handler_HappyPath(t *testing.T) {
	// Arrange
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

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
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB)(ctx, mockLogger, args)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, runmatlabbuild.ReturnArgs{
		Message:                 "Build triggered successfully",
		BuildGoroutineStarted:   true,
		ProgressStreamConnected: false,
		ProgressStreamError:     "MATLAB progress stream is not configured in this prototype",
	}, result)
}

func TestTool_Handler_ClientReturnsError(t *testing.T) {
	// Arrange
	mockUsecase := &mocks.MockUsecase{}
	defer mockUsecase.AssertExpectations(t)

	mockGlobalMATLAB := &entitiesmocks.MockGlobalMATLAB{}
	defer mockGlobalMATLAB.AssertExpectations(t)

	mockLogger := testutils.NewInspectableLogger()
	ctx := t.Context()
	expectedError := assert.AnError

	mockGlobalMATLAB.EXPECT().
		Client(ctx, mockLogger.AsMockArg()).
		Return(nil, expectedError).
		Once()

	// Act
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB)(ctx, mockLogger, runmatlabbuild.Args{
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
	result, err := runmatlabbuild.Handler(mockUsecase, mockGlobalMATLAB)(ctx, mockLogger, runmatlabbuild.Args{
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

	// Act
	tool := runmatlabbuild.New(mockLoggerFactory, mockTelemetryFactory, mockUsecase, mockGlobalMATLAB)

	// Assert
	assert.Equal(t, annotations.NewDestructiveAnnotations(), tool.Annotations())
}
