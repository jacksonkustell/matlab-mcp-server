function sendMCPProgressNotification(message)
%sendMCPProgressNotification Broadcast a progress notification to MCP clients.

% Copyright 2026 The MathWorks, Inc.

try
    key = progressEndpointsAppDataKey();
    if ~isappdata(groot, key)
        return
    end

    endpoints = getappdata(groot, key);
    if ~isa(endpoints, "containers.Map")
        rmappdata(groot, key);
        return
    end

    payload = struct( ...
        "timestamp", utcTimestamp(), ...
        "message", char(string(message)) ...
    );
    notification = jsonencode(payload);

    registrationIDs = keys(endpoints);
    failedRegistrationIDs = strings(0, 1);
    for index = 1:numel(registrationIDs)
        registrationID = registrationIDs{index};
        try
            writeline(endpoints(registrationID), notification);
        catch
            failedRegistrationIDs(end + 1, 1) = string(registrationID); %#ok<AGROW>
        end
    end

    for index = 1:numel(failedRegistrationIDs)
        registrationID = char(failedRegistrationIDs(index));
        if isKey(endpoints, registrationID)
            client = endpoints(registrationID);
            remove(endpoints, registrationID);
            safelyDelete(client);
        end
    end

    if endpoints.Count == 0
        rmappdata(groot, key);
    else
        setappdata(groot, key, endpoints);
    end
catch
    % Progress reporting must not interrupt MATLAB user code.
end
end

function timestamp = utcTimestamp()
timestamp = char(datetime( ...
    "now", ...
    TimeZone="UTC", ...
    Format="uuuu-MM-dd'T'HH:mm:ss.SSS'Z'" ...
));
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
