package wsproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"nhooyr.io/websocket"
)

// TODO: make this configurable?
const (
	ConnectionIDHeaderKey = "X-WSGW-CONNECTION-ID"
	connIdPathParamName   = ConnectionIDKey
)

type wsIOAdapter struct {
	wsConn *websocket.Conn
}

func (wsIo *wsIOAdapter) CloseRead(ctx context.Context) context.Context {
	return wsIo.wsConn.CloseRead(ctx)
}

func (wsIo *wsIOAdapter) Close() error {
	return wsIo.wsConn.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
}

func (wsIo *wsIOAdapter) Write(ctx context.Context, msg string) error {
	return wsIo.wsConn.Write(ctx, websocket.MessageText, []byte(msg))
}

func (wsIo *wsIOAdapter) Read(ctx context.Context) (string, error) {
	msgType, msg, err := wsIo.wsConn.Read(ctx)
	if err != nil {
		return "", err
	}
	if msgType != websocket.MessageText {
		return "", errors.New("unexpected message type")
	}
	return string(msg), nil
}

type applicationURLs interface {
	connecting() string
	disconnected() string
	message() string
}

type appConnection struct {
	id         ConnectionID
	httpClient http.Client
}

// Relays the connection request to the backend's `POST /ws/connect` endpoint and
func handleClientConnecting(createConnectionId func() ConnectionID, appUrls applicationURLs) func(c *gin.Context) *appConnection {
	return func(g *gin.Context) *appConnection {
		logger := zerolog.Ctx(g.Request.Context()).With().Str("method", fmt.Sprintf("handleClientConnecting: %s", appUrls.connecting())).Logger()

		request, err := http.NewRequest(http.MethodGet, appUrls.connecting(), nil)
		if err != nil {
			logger.Error().Msgf("failed to create request object: %v", err)
			g.AbortWithStatus(http.StatusInternalServerError)
			return nil
		}
		request.Header = g.Request.Header

		connId := createConnectionId()

		request.Header.Add(ConnectionIDHeaderKey, string(connId))

		client := http.Client{
			Timeout: time.Second * 15,
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			logger.Error().Msgf("failed to send request: %v", requestErr)
			g.AbortWithStatus(http.StatusInternalServerError)
			return nil
		}
		defer cleanupResponse(response)

		if response.StatusCode == http.StatusUnauthorized {
			logger.Info().Msg("Authentication failed")
			g.AbortWithStatus(http.StatusUnauthorized)
			return nil
		}

		if response.StatusCode != 200 {
			logger.Info().Msgf("Received status code %d", response.StatusCode)
			g.AbortWithStatus(http.StatusInternalServerError)
			return nil
		}

		logger.Debug().Msgf("app has accepted: %v", connId)

		return &appConnection{connId, client}
	}
}

func handleClientDisconnected(appUrls applicationURLs, appConn *appConnection, logger zerolog.Logger) {
	logger = logger.With().Str("method", "handleClientDisconnected").Str("appUrl", appUrls.disconnected()).Str(ConnectionIDKey, string(appConn.id)).Logger()

	logger.Debug().Msg("BEGIN")

	request, err := http.NewRequest(http.MethodPost, appUrls.disconnected(), nil)
	if err != nil {
		logger.Error().Msgf("failed to create request object: %v", err)
		return
	}
	request.Header.Add(ConnectionIDHeaderKey, string(appConn.id))

	response, requestErr := appConn.httpClient.Do(request)
	if requestErr != nil {
		logger.Error().Msgf("failed to send request: %v", requestErr)
		return
	}
	defer cleanupResponse(response)

	if response.StatusCode != 200 {
		logger.Info().Msgf("Received status code %d", response.StatusCode)
		return
	}
}

// Calls the `POST /ws/message-received` endpoint on the backend with "msg" and ConnectionIDKey
func handleClientMessage(appConn *appConnection, appUrls applicationURLs) func(c context.Context, msg string) error {
	return func(c context.Context, msg string) error {
		logger := zerolog.Ctx(c).With().Str(ConnectionIDKey, string(appConn.id)).Str("func", "handleClientMessage").Logger()
		logger.Debug().Str("msg", msg).Send()

		request, err := http.NewRequest(
			http.MethodPost,
			appUrls.message(),
			bytes.NewReader([]byte(msg)),
		)
		if err != nil {
			logger.Error().Msgf("failed to create request object: %v", err)
			return err
		}
		request.Header.Add(ConnectionIDHeaderKey, string(appConn.id))

		response, requestErr := appConn.httpClient.Do(request)
		if requestErr != nil {
			logger.Error().Msgf("failed to send request: %v", requestErr)
			return requestErr
		}
		defer cleanupResponse(response)

		if response.StatusCode != 200 {
			logger.Info().Msgf("Received status code %d", response.StatusCode)
			return fmt.Errorf("probelm while sending message to application")
		}

		return nil
	}
}

func sendMessageToClient(ctx context.Context, wsconn *websocket.Conn, obj any) error {
	jsonBytes, marshalErr := json.Marshal(obj)
	if marshalErr != nil {
		return marshalErr
	}
	writeErr := wsconn.Write(ctx, websocket.MessageText, jsonBytes)
	if writeErr != nil {
		return writeErr
	}
	return nil
}

// connectHandler calls `authenticateClient` if it is not `nil` to authenticate the client,
// then notifies the application of the new WS connection
func connectHandler(
	appUrls applicationURLs,
	ws *wsConnections,
	loadBalancerAddress string,
	createConnectionId func() ConnectionID,
	clusterSupport *ClusterSupport,
) gin.HandlerFunc {
	return func(g *gin.Context) {
		appConn := handleClientConnecting(createConnectionId, appUrls)(g)

		if appConn == nil {
			return
		}

		logger := zerolog.Ctx(g.Request.Context()).With().Str("method", "authentication handler").Str(ConnectionIDKey, string(appConn.id)).Logger()

		// logger = logger.().Str("method", "connectHandler").Str(ConnectionIDKey, string(appConn.id)).Logger()

		wsConn, subsErr := websocket.Accept(g.Writer, g.Request, &websocket.AcceptOptions{
			OriginPatterns: []string{loadBalancerAddress},
		})
		if subsErr != nil {
			logger.Error().Msgf("Failed to accept WS connection request: %v", subsErr)
			_ = g.Error(subsErr)
			g.AbortWithStatus(500)
			return
		}

		var wsClosedError error
		defer func() {
			wsConn.Close(websocket.StatusNormalClosure, "")

			handleClientDisconnected(appUrls, appConn, logger)

			if clusterSupport != nil {
				clusterSupport.deregisterConnection(g.Request.Context(), appConn.id)
			}

			if wsClosedError != nil {
				if errors.Is(wsClosedError, context.Canceled) {
					return // Done
				}

				if websocket.CloseStatus(wsClosedError) == websocket.StatusNormalClosure ||
					websocket.CloseStatus(wsClosedError) == websocket.StatusGoingAway {
					return
				}

				logger.Error().Msgf("%v", wsClosedError)
			}
		}()

		if clusterSupport != nil {
			clusterSupport.registerConnection(g.Request.Context(), appConn.id)
		}

		ackErr := sendMessageToClient(g.Request.Context(), wsConn, map[string]string{ConnectionIDKey: string(appConn.id)})
		if ackErr != nil {
			logger.Error().Err(fmt.Errorf("failed to send connect ack: %v", ackErr))
			wsClosedError = ackErr
			return
		}

		logger.Debug().Msg("websocket message processing about to start...")

		wsClosedError = ws.processMessages(g.Request.Context(), appConn.id, &wsIOAdapter{wsConn}, handleClientMessage(appConn, appUrls)) // we block here until Error or Done

		logger.Debug().Msgf("websocket message processing finished with %v", wsClosedError)
	}
}

func pushHandler(authenticateBackend func(c *gin.Context) error, ws *wsConnections, clusterSupport *ClusterSupport) gin.HandlerFunc {
	return func(g *gin.Context) {
		connectionIdStr := g.Param(connIdPathParamName)

		logger := zerolog.Ctx(g.Request.Context()).With().Str("method", "pushHandler").Str(ConnectionIDKey, connectionIdStr).Logger()

		if connectionIdStr == "" {
			logger.Info().Msgf("Missing path param: %s", connIdPathParamName)
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}

		requestBody, errReadRequest := io.ReadAll(g.Request.Body)
		g.Request.Body.Close()
		if errReadRequest != nil {
			logger.Error().Msgf("failed to read request body %T: %v", g.Request.Body, errReadRequest)
			g.JSON(500, nil)
			return
		}

		bodyAsString := string(requestBody)

		errPush := ws.push(g.Request.Context(), bodyAsString, ConnectionID(connectionIdStr))
		if errPush == errConnectionNotFound {
			if clusterSupport == nil {
				logger.Info().Msg("Web-socket connection not found")
				g.AbortWithStatus(http.StatusNotFound)
				return
			}
			logger.Info().Msgf("Connection '%s' isn't managed here, publishing payload...", connectionIdStr)
			errPush = clusterSupport.relayMessage(g.Request.Context(), ConnectionID(connectionIdStr), bodyAsString)
		}

		if errPush != nil {
			logger.Error().Msgf("Failed to push to connection %s: %v", connectionIdStr, errPush)
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		g.Status(http.StatusNoContent)
	}
}

func cleanupResponse(response *http.Response) {
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
}
