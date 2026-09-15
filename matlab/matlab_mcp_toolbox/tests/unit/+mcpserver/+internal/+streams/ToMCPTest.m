classdef ToMCPTest < matlab.unittest.TestCase
    %ToMCPTest Tests for mcpserver.internal.streams.ToMCP.

    % Copyright 2026 The MathWorks, Inc.

    methods (TestMethodSetup)
        function resetProgressEndpoints(testCase)
            clearProgressEndpoints();
            testCase.addTeardown(@clearProgressEndpoints);
        end
    end

    methods (Test)
        function testPrintSendsFormattedMessageToMCP(testCase)
            % Arrange
            [server, closeServer] = createLoopbackServer();
            testCase.addTeardown(closeServer);
            matlab_mcp.registerMCPProgressEndpoint( ...
                "to-mcp", "127.0.0.1", server.getLocalPort() ...
            );
            socket = acceptSocket(server);
            testCase.addTeardown(@() closeSocket(socket));
            stream = mcpserver.internal.streams.ToMCP();

            % Act
            stream.print("Completed %d of %d tasks", 2, 3);
            payload = decodeNotification(readLineIncludingLF(socket));

            % Assert
            testCase.verifyEqual( ...
                string(payload.message), "Completed 2 of 3 tasks" ...
            );
        end
    end
end

function [server, closeServer] = createLoopbackServer()
address = java.net.InetAddress.getByName("127.0.0.1");
server = java.net.ServerSocket(0, 50, address);
closeServer = @() closeServerSocket(server);
end

function socket = acceptSocket(server)
server.setSoTimeout(2000);
socket = server.accept();
socket.setSoTimeout(2000);
end

function closeServerSocket(server)
if ~server.isClosed()
    server.close();
end
end

function closeSocket(socket)
if ~socket.isClosed()
    socket.close();
end
end

function bytes = readLineIncludingLF(socket)
inputStream = socket.getInputStream();
bytes = zeros(1, 0, "uint8");

while true
    nextByte = inputStream.read();
    if nextByte == -1
        error( ...
            "ToMCPTest:ConnectionClosed", ...
            "Socket closed before a newline-delimited notification arrived." ...
        );
    end

    bytes(end + 1) = uint8(nextByte); %#ok<AGROW>
    if nextByte == 10
        return
    end
end
end

function payload = decodeNotification(bytes)
text = native2unicode(bytes(1:end - 1), "UTF-8");
payload = jsondecode(text);
end

function clearProgressEndpoints()
key = progressEndpointsAppDataKey();
if ~isappdata(groot, key)
    return
end

endpoints = getappdata(groot, key);
if isa(endpoints, "containers.Map")
    registrationIDs = keys(endpoints);
    for index = 1:numel(registrationIDs)
        matlab_mcp.unregisterMCPProgressEndpoint(registrationIDs{index});
    end
end

if isappdata(groot, key)
    rmappdata(groot, key);
end
end

function key = progressEndpointsAppDataKey()
key = "matlab_mcp_progressEndpoints";
end
