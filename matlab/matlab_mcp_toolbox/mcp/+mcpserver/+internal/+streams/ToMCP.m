classdef ToMCP < matlab.automation.streams.OutputStream
    %ToMCP Send formatted output to registered MCP progress endpoints.

    % Copyright 2026 The MathWorks, Inc.

    methods
        function print(~, formatSpec, varargin)
            matlab_mcp.sendMCPProgressNotification( ...
                sprintf(formatSpec, varargin{:}) ...
            );
        end
    end
end
