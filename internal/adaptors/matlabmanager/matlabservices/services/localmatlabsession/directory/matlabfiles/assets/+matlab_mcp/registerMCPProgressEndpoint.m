function registerMCPProgressEndpoint(registrationID, host, port)
%registerMCPProgressEndpoint Register an MCP progress notification endpoint.

% Copyright 2026 The MathWorks, Inc.

registrationID = char(string(registrationID));
host = char(string(host));
port = normalizePort(port);

endpoints = getEndpoints();
if isKey(endpoints, registrationID)
    previousClient = endpoints(registrationID);
    remove(endpoints, registrationID);
    safelyDelete(previousClient);
end

client = tcpclient(host, port, Timeout=1, ConnectTimeout=1);
configureTerminator(client, "LF");
endpoints(registrationID) = client;
setappdata(groot, progressEndpointsAppDataKey(), endpoints);
end

function endpoints = getEndpoints()
key = progressEndpointsAppDataKey();
if isappdata(groot, key)
    endpoints = getappdata(groot, key);
    if isa(endpoints, "containers.Map")
        return
    end
end

endpoints = containers.Map(KeyType="char", ValueType="any");
end

function port = normalizePort(port)
if ischar(port) || isstring(port)
    port = str2double(port);
end

validateattributes(port, {'numeric'}, {'scalar', 'finite', 'integer', 'positive', '<=', 65535});
end

function key = progressEndpointsAppDataKey()
key = "matlab_mcp_progressEndpoints";
end

function safelyDelete(client)
try
    delete(client);
catch
end
end
