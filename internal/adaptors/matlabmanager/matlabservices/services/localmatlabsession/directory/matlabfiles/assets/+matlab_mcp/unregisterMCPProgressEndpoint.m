function unregisterMCPProgressEndpoint(registrationID)
%unregisterMCPProgressEndpoint Remove an MCP progress notification endpoint.

% Copyright 2026 The MathWorks, Inc.

key = progressEndpointsAppDataKey();
if ~isappdata(groot, key)
    return
end

endpoints = getappdata(groot, key);
if ~isa(endpoints, "containers.Map")
    rmappdata(groot, key);
    return
end

registrationID = char(string(registrationID));
if isKey(endpoints, registrationID)
    client = endpoints(registrationID);
    remove(endpoints, registrationID);
    safelyDelete(client);
end

if endpoints.Count == 0
    rmappdata(groot, key);
else
    setappdata(groot, key, endpoints);
end
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
