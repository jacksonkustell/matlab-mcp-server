classdef (Abstract) MCPBuildToolPluginFactory
    %MCPBuildToolPluginFactory Create Build Tool plugins that report to MCP.

    % Copyright 2026 The MathWorks, Inc.

    methods (Abstract)
        plugins = createPlugins(factory, verbosity)
    end
end
