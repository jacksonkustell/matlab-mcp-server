// Copyright 2026 The MathWorks, Inc.

package runmatlabbuildfile_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/testutils"
	"github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
	entitiesmocks "github.com/matlab/matlab-mcp-server/mocks/entities"
	mocks "github.com/matlab/matlab-mcp-server/mocks/usecases/runmatlabbuildfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNew_HappyPath(t *testing.T) {
	// Arrange
	mockPathValidator := &mocks.MockPathValidator{}
	defer mockPathValidator.AssertExpectations(t)

	// Act
	usecase := runmatlabbuildfile.New(mockPathValidator)

	// Assert
	assert.NotNil(t, usecase, "Usecase should not be nil")
}

func TestBuildEvalRequest_EscapesSingleQuotesInValidatedBuildfilePath(t *testing.T) {
	// Arrange
	args := runmatlabbuildfile.Args{
		BuildfilePath: `C:\Users\O'Brien\project\buildfile.m`,
	}

	// Act
	result := runmatlabbuildfile.BuildEvalRequest(args)

	// Assert
	assert.Equal(t, entities.EvalRequest{
		Code: `buildtool('-buildFile', 'C:\Users\O''Brien\project\buildfile.m')`,
	}, result)
}

func TestUsecase_Execute_ValidationErrorDoesNotLaunchBuild(t *testing.T) {
	// Arrange
	mockLogger := testutils.NewInspectableLogger()

	mockPathValidator := &mocks.MockPathValidator{}
	defer mockPathValidator.AssertExpectations(t)

	mockClient := &entitiesmocks.MockMATLABSessionClient{}
	defer mockClient.AssertExpectations(t)

	ctx := t.Context()
	const buildfilePath = "/project/buildfile.m"
	expectedError := assert.AnError

	mockPathValidator.EXPECT().
		ValidateMATLABScript(buildfilePath).
		Return("", expectedError).
		Once()

	usecase := runmatlabbuildfile.New(mockPathValidator)

	// Act
	err := usecase.Execute(ctx, mockLogger, mockClient, runmatlabbuildfile.Args{
		BuildfilePath: buildfilePath,
	})

	// Assert
	require.ErrorIs(t, err, expectedError)
	mockClient.AssertNotCalled(t, "Eval", mock.Anything, mock.Anything, mock.Anything)
}

func TestUsecase_Execute_ReturnsBeforeBackgroundEvaluationCompletes(t *testing.T) {
	// Arrange
	mockLogger := testutils.NewInspectableLogger()

	mockPathValidator := &mocks.MockPathValidator{}
	defer mockPathValidator.AssertExpectations(t)

	mockClient := &entitiesmocks.MockMATLABSessionClient{}
	defer mockClient.AssertExpectations(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	const (
		buildfilePath          = "/project/buildfile.m"
		validatedBuildfilePath = "/validated/project/buildfile.m"
	)
	expectedRequest := entities.EvalRequest{
		Code: "buildtool('-buildFile', '/validated/project/buildfile.m')",
	}
	expectedError := assert.AnError
	evaluationStarted := make(chan context.Context, 1)
	releaseEvaluation := make(chan struct{})
	evaluationCompleted := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseEvaluation)
		})
	})

	mockPathValidator.EXPECT().
		ValidateMATLABScript(buildfilePath).
		Return(validatedBuildfilePath, nil).
		Once()

	mockClient.EXPECT().
		Eval(mock.Anything, mockLogger.AsMockArg(), expectedRequest).
		RunAndReturn(func(backgroundContext context.Context, _ entities.Logger, _ entities.EvalRequest) (entities.EvalResponse, error) {
			evaluationStarted <- backgroundContext
			<-releaseEvaluation
			close(evaluationCompleted)
			return entities.EvalResponse{}, expectedError
		}).
		Once()

	usecase := runmatlabbuildfile.New(mockPathValidator)
	executeCompleted := make(chan error, 1)

	// Act
	go func() {
		executeCompleted <- usecase.Execute(ctx, mockLogger, mockClient, runmatlabbuildfile.Args{
			BuildfilePath: buildfilePath,
		})
	}()

	// Assert
	select {
	case err := <-executeCompleted:
		require.NoError(t, err, "Execute should return while Eval remains blocked")
	case <-time.After(time.Second):
		t.Fatal("Execute did not return while Eval was blocked")
	}

	var backgroundContext context.Context
	select {
	case backgroundContext = <-evaluationStarted:
	case <-time.After(time.Second):
		t.Fatal("background Eval was not launched")
	}

	cancel()
	assert.NoError(t, backgroundContext.Err(), "background context should survive caller cancellation")
	assert.Nil(t, backgroundContext.Done(), "background context should not expose caller cancellation")

	releaseOnce.Do(func() {
		close(releaseEvaluation)
	})
	select {
	case <-evaluationCompleted:
	case <-time.After(time.Second):
		t.Fatal("background Eval did not complete after it was released")
	}

	require.Eventually(t, func() bool {
		logFields, logged := mockLogger.ErrorLogs()["MATLAB build evaluation failed"]
		if !logged {
			return false
		}

		loggedError, isError := logFields["error"].(error)
		return isError && errors.Is(loggedError, expectedError)
	}, time.Second, 10*time.Millisecond, "background Eval error should be logged")
}
