// Copyright 2026 The MathWorks, Inc.

package queuematlabcommand_test

import (
	"errors"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/singlesession/queuematlabcommand"
	"github.com/matlab/matlab-mcp-server/internal/testutils"
	"github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTool_MetadataAndSchemas(t *testing.T) {
	tool := queuematlabcommand.New(nil, nil, nil)

	inputSchema, err := tool.GetInputSchema()
	require.NoError(t, err)
	expectedInputSchema, err := jsonschema.For[queuematlabcommand.Args](&jsonschema.ForOptions{})
	require.NoError(t, err)

	outputSchema, err := tool.GetOutputSchema()
	require.NoError(t, err)
	expectedOutputSchema, err := jsonschema.For[queuematlabcommand.ReturnArgs](&jsonschema.ForOptions{})
	require.NoError(t, err)

	assert.Equal(t, "queue_matlab_command", tool.Name())
	assert.Equal(t, "Queue MATLAB Command", tool.Title())
	assert.Equal(t, annotations.NewDestructiveAnnotations(), tool.Annotations())
	assert.Equal(t, expectedInputSchema, inputSchema)
	assert.Equal(t, expectedOutputSchema, outputSchema)
}

func TestHandler_QueuesCommand(t *testing.T) {
	usecase := &queueUsecaseStub{
		result: matlabcommandqueue.EnqueueResult{
			CommandID: "command-1",
			Status:    matlabcommandqueue.StatusQueued,
			Message:   "Command queued.",
		},
	}
	logger := testutils.NewInspectableLogger()

	result, err := queuematlabcommand.Handler(usecase)(t.Context(), logger, queuematlabcommand.Args{
		Code:        "disp('queued')",
		ProjectPath: `C:\project`,
	})

	require.NoError(t, err)
	assert.Equal(t, "disp('queued')", usecase.code)
	assert.Equal(t, `C:\project`, usecase.projectPath)
	assert.Equal(t, queuematlabcommand.ReturnArgs{
		CommandID: "command-1",
		Status:    "queued",
		Message:   "Command queued.",
	}, result)
}

func TestHandler_ReturnsValidationError(t *testing.T) {
	expectedError := errors.New("code must not be blank")
	usecase := &queueUsecaseStub{err: expectedError}
	logger := testutils.NewInspectableLogger()

	result, err := queuematlabcommand.Handler(usecase)(t.Context(), logger, queuematlabcommand.Args{})

	require.ErrorIs(t, err, expectedError)
	assert.Empty(t, result)
}

type queueUsecaseStub struct {
	code        string
	projectPath string
	result      matlabcommandqueue.EnqueueResult
	err         error
}

func (s *queueUsecaseStub) Enqueue(code string, projectPath string) (matlabcommandqueue.EnqueueResult, error) {
	s.code = code
	s.projectPath = projectPath
	return s.result, s.err
}
