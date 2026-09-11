// Copyright 2026 The MathWorks, Inc.

package pollmatlabcommands_test

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/singlesession/pollmatlabcommands"
	"github.com/matlab/matlab-mcp-server/internal/testutils"
	"github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTool_MetadataAndSchemas(t *testing.T) {
	tool := pollmatlabcommands.New(nil, nil, nil)

	inputSchema, err := tool.GetInputSchema()
	require.NoError(t, err)
	expectedInputSchema, err := jsonschema.For[pollmatlabcommands.Args](&jsonschema.ForOptions{})
	require.NoError(t, err)

	outputSchema, err := tool.GetOutputSchema()
	require.NoError(t, err)
	expectedOutputSchema, err := jsonschema.For[pollmatlabcommands.ReturnArgs](&jsonschema.ForOptions{})
	require.NoError(t, err)

	assert.Equal(t, "poll_matlab_commands", tool.Name())
	assert.Equal(t, "Poll MATLAB Commands", tool.Title())
	assert.Equal(t, annotations.NewReadOnlyAnnotations(), tool.Annotations())
	assert.Equal(t, expectedInputSchema, inputSchema)
	assert.Equal(t, expectedOutputSchema, outputSchema)
}

func TestHandler_ReturnsCommandSnapshots(t *testing.T) {
	commands := []matlabcommandqueue.Command{
		{
			CommandID:     "command-1",
			Code:          "disp('done')",
			Status:        matlabcommandqueue.StatusCompleted,
			Output:        "done\n",
			StatusMessage: "completed",
		},
	}
	usecase := &pollUsecaseStub{commands: commands}
	logger := testutils.NewInspectableLogger()

	result, err := pollmatlabcommands.Handler(usecase)(t.Context(), logger, pollmatlabcommands.Args{})

	require.NoError(t, err)
	assert.Equal(t, commands, result.Commands)
	assert.Equal(t, 1, usecase.calls)
}

func TestHandler_ReturnsEmptyCommandsArray(t *testing.T) {
	usecase := &pollUsecaseStub{commands: []matlabcommandqueue.Command{}}
	logger := testutils.NewInspectableLogger()

	result, err := pollmatlabcommands.Handler(usecase)(t.Context(), logger, pollmatlabcommands.Args{})

	require.NoError(t, err)
	assert.NotNil(t, result.Commands)
	assert.Empty(t, result.Commands)
}

type pollUsecaseStub struct {
	commands []matlabcommandqueue.Command
	calls    int
}

func (s *pollUsecaseStub) Poll() []matlabcommandqueue.Command {
	s.calls++
	return s.commands
}
