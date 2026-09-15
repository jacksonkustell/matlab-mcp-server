classdef MCPBuildToolPluginService < ...
        matlab.buildtool.internal.services.plugins.BuildRunnerPluginService
    %MCPBuildToolPluginService Provide Build Tool plugins that report to MCP.

    % Copyright 2026 The MathWorks, Inc.

    properties (GetAccess = private, SetAccess = immutable)
        Factory(1, 1) matlab.buildtool.internal.services.plugins.MCPBuildToolPluginFactory = ...
            matlab.buildtool.internal.services.plugins.DefaultMCPBuildToolPluginFactory()
    end

    methods
        function service = MCPBuildToolPluginService(options)
            arguments
                options.?matlab.buildtool.internal.services.plugins.MCPBuildToolPluginService
            end

            for property = string(fieldnames(options).')
                service.(property) = options.(property);
            end
        end

        function plugins = providePlugins(service, options)
            arguments
                service
                options (1, 1) struct
            end

            if isfield(options, "Verbosity")
                verbosity = options.Verbosity;
            else
                verbosity = matlab.automation.Verbosity.Concise;
            end

            plugins = service.Factory.createPlugins(verbosity);
        end
    end
end
