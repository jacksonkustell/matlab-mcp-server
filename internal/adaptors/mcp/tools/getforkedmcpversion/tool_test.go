// Copyright 2026 The MathWorks, Inc.

package getforkedmcpversion_test

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/getforkedmcpversion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTool_MetadataAndEmptyInputSchema(t *testing.T) {
	// Act
	tool := getforkedmcpversion.New(nil, nil)
	inputSchema, err := tool.GetInputSchema()

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "get_forked_mcp_version", tool.Name())
	assert.Equal(t, "Get Forked MCP Version", tool.Title())
	assert.Equal(t, "Returns the forked MCP version.", tool.Description())
	assert.Equal(t, annotations.NewReadOnlyAnnotations(), tool.Annotations())

	expectedSchema, err := jsonschema.For[getforkedmcpversion.Args](&jsonschema.ForOptions{})
	require.NoError(t, err)
	assert.Equal(t, expectedSchema, inputSchema)
}

func TestHandler_ReturnsForkedMCPVersion(t *testing.T) {
	// Act
	result, err := getforkedmcpversion.Handler()(t.Context(), nil, getforkedmcpversion.Args{})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"0.06"}, result.TextContent)
	assert.Empty(t, result.ImageContent)
}
