classdef MCPProgressNotificationTest < matlab.unittest.TestCase
    %MCPProgressNotificationTest Tests TCP progress notification helpers.

    % Copyright 2026 The MathWorks, Inc.

    methods (TestMethodSetup)
        function resetProgressEndpoints(testCase)
            clearProgressEndpoints();
            testCase.addTeardown(@clearProgressEndpoints);
        end
    end

    methods (Test)
        function testSendNotificationWritesLFDelimitedJSON(testCase)
            % Arrange
            [server, closeServer] = createLoopbackServer();
            testCase.addTeardown(closeServer);
            registrationID = "valid-json";

            matlab_mcp.registerMCPProgressEndpoint(registrationID, "127.0.0.1", server.getLocalPort());
            socket = acceptSocket(server);
            testCase.addTeardown(@() closeSocket(socket));

            % Act
            matlab_mcp.sendMCPProgressNotification("running step 1");
            bytes = readLineIncludingLF(socket);
            payload = decodeNotification(bytes);

            % Assert
            testCase.verifyEqual(bytes(end), uint8(10));
            testCase.verifyNotEqual(bytes(end - 1), uint8(13));
            testCase.verifyEqual(string(payload.message), "running step 1");
            timestamp = datetime( ...
                string(payload.timestamp), ...
                InputFormat="uuuu-MM-dd'T'HH:mm:ss.SSS'Z'", ...
                TimeZone="UTC" ...
            );
            testCase.verifyEqual(string(timestamp.TimeZone), "UTC");
        end

        function testSendNotificationBroadcastsToEveryRegisteredEndpoint(testCase)
            % Arrange
            [firstServer, closeFirstServer] = createLoopbackServer();
            testCase.addTeardown(closeFirstServer);
            [secondServer, closeSecondServer] = createLoopbackServer();
            testCase.addTeardown(closeSecondServer);

            matlab_mcp.registerMCPProgressEndpoint("first", "127.0.0.1", firstServer.getLocalPort());
            firstSocket = acceptSocket(firstServer);
            testCase.addTeardown(@() closeSocket(firstSocket));
            matlab_mcp.registerMCPProgressEndpoint("second", "127.0.0.1", secondServer.getLocalPort());
            secondSocket = acceptSocket(secondServer);
            testCase.addTeardown(@() closeSocket(secondSocket));

            % Act
            matlab_mcp.sendMCPProgressNotification("shared progress");
            firstPayload = decodeNotification(readLineIncludingLF(firstSocket));
            secondPayload = decodeNotification(readLineIncludingLF(secondSocket));

            % Assert
            testCase.verifyEqual(string(firstPayload.message), "shared progress");
            testCase.verifyEqual(string(secondPayload.message), "shared progress");
        end

        function testReregisterReplacesEndpointAndUnregisterRemovesIt(testCase)
            % Arrange
            [firstServer, closeFirstServer] = createLoopbackServer();
            testCase.addTeardown(closeFirstServer);
            [replacementServer, closeReplacementServer] = createLoopbackServer();
            testCase.addTeardown(closeReplacementServer);
            registrationID = "replace-me";

            matlab_mcp.registerMCPProgressEndpoint(registrationID, "127.0.0.1", firstServer.getLocalPort());
            firstSocket = acceptSocket(firstServer);
            testCase.addTeardown(@() closeSocket(firstSocket));
            matlab_mcp.registerMCPProgressEndpoint(registrationID, "127.0.0.1", replacementServer.getLocalPort());
            replacementSocket = acceptSocket(replacementServer);
            testCase.addTeardown(@() closeSocket(replacementSocket));

            % Act
            matlab_mcp.sendMCPProgressNotification("replacement progress");
            payload = decodeNotification(readLineIncludingLF(replacementSocket));
            matlab_mcp.unregisterMCPProgressEndpoint(registrationID);

            % Assert
            testCase.verifyEqual(string(payload.message), "replacement progress");
            testCase.verifyFalse(isappdata(groot, progressEndpointsAppDataKey()));
        end

        function testFailedEndpointDoesNotPreventOtherEndpointDelivery(testCase)
            % Arrange
            [failedServer, closeFailedServer] = createLoopbackServer();
            testCase.addTeardown(closeFailedServer);
            [workingServer, closeWorkingServer] = createLoopbackServer();
            testCase.addTeardown(closeWorkingServer);

            matlab_mcp.registerMCPProgressEndpoint("failed", "127.0.0.1", failedServer.getLocalPort());
            failedSocket = acceptSocket(failedServer);
            testCase.addTeardown(@() closeSocket(failedSocket));
            closeSocketWithReset(failedSocket);
            matlab_mcp.registerMCPProgressEndpoint("working", "127.0.0.1", workingServer.getLocalPort());
            workingSocket = acceptSocket(workingServer);
            testCase.addTeardown(@() closeSocket(workingSocket));

            % Act
            matlab_mcp.sendMCPProgressNotification("first notification");
            firstPayload = decodeNotification(readLineIncludingLF(workingSocket));
            endpoints = getappdata(groot, progressEndpointsAppDataKey());
            matlab_mcp.sendMCPProgressNotification("second notification");
            secondPayload = decodeNotification(readLineIncludingLF(workingSocket));

            % Assert
            testCase.verifyEqual(string(firstPayload.message), "first notification");
            testCase.verifyFalse(isKey(endpoints, "failed"));
            testCase.verifyTrue(isKey(endpoints, "working"));
            testCase.verifyEqual(string(secondPayload.message), "second notification");
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

function closeSocketWithReset(socket)
socket.setSoLinger(true, 0);
closeSocket(socket);
end

function bytes = readLineIncludingLF(socket)
inputStream = socket.getInputStream();
bytes = zeros(1, 0, "uint8");

while true
    nextByte = inputStream.read();
    if nextByte == -1
        error("MCPProgressNotificationTest:ConnectionClosed", "Socket closed before a newline-delimited notification arrived.");
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
