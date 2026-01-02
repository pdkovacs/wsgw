package wsgw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"wsgw/internal/config"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/monitoring"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
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

var httpClient http.Client = http.Client{
	Timeout: time.Second * 15,
}

type appConnection struct {
	id         ConnectionID
	httpClient http.Client
}

var errAppConnInternal = errors.New("internalError")
var errAppConnAuthn = errors.New("authnError")
var errAppConnAccepting = errors.New("appError")

// Relays the connection request to the backend's `POST /ws/connect` endpoint and
func handleClientConnecting(requestCtx context.Context, r *http.Request, createConnectionId func(ctx context.Context) ConnectionID, appUrls applicationURLs) (*appConnection, error) {
	logger := zerolog.Ctx(r.Context()).With().Str(logging.MethodLogger, "handleClientConnecting").Logger()

	request, err := http.NewRequest(http.MethodGet, appUrls.connecting(), nil)
	if err != nil {
		logger.Error().Msgf("failed to create request object: %v", err)
		return nil, errAppConnInternal
	}
	request.Header = r.Header

	connId := createConnectionId(r.Context())

	request.Header.Add(ConnectionIDHeaderKey, string(connId))

	monitoring.InjectIntoHeader(requestCtx, request.Header)

	response, requestErr := httpClient.Do(request)
	if requestErr != nil {
		logger.Error().Msgf("failed to send request: %v", requestErr)
		return nil, errAppConnInternal
	}
	defer cleanupResponse(response)

	if response.StatusCode == http.StatusUnauthorized {
		logger.Info().Msg("Authentication failed")
		return nil, errAppConnAuthn
	}

	if response.StatusCode != 200 {
		logger.Info().Msgf("Received status code %d", response.StatusCode)
		return nil, errAppConnAccepting
	}

	logger.Debug().Msgf("app has accepted: %v", connId)

	return &appConnection{connId, httpClient}, nil
}

func handleClientDisconnected(ctx context.Context, appUrls applicationURLs, connReqHeader http.Header, appConn *appConnection, logger zerolog.Logger) {
	logger = logger.With().Str(logging.MethodLogger, "handleClientDisconnected").Str("appUrl", appUrls.disconnected()).Str(ConnectionIDKey, string(appConn.id)).Logger()

	logger.Debug().Msg("BEGIN")

	request, err := http.NewRequest(http.MethodPost, appUrls.disconnected(), nil)
	if err != nil {
		logger.Error().Msgf("failed to create request object: %v", err)
		return
	}
	request.Header = connReqHeader
	request.Header.Add(ConnectionIDHeaderKey, string(appConn.id))

	monitoring.InjectIntoHeader(ctx, request.Header)

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

	logger.Debug().Msg("END")
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
	createConnectionId func(ctx context.Context) ConnectionID,
	ackWithNewConnId bool,
	clusterSupport *ClusterSupport,
) gin.HandlerFunc {
	return func(g *gin.Context) {
		requestContext := monitoring.ExtractFromHeader(g.Request.Context(), g.Request.Header)
		tracer := otel.Tracer(config.OtelScope)
		tmpCtx, span := tracer.Start(requestContext, "new-ws-connection")
		requestContext = tmpCtx

		appConn, clientConnectErr := handleClientConnecting(requestContext, g.Request, createConnectionId, appUrls)

		span.End()

		if clientConnectErr != nil {
			switch clientConnectErr {
			case errAppConnAccepting:
			case errAppConnInternal:
				g.AbortWithStatus(http.StatusInternalServerError)
			case errAppConnAuthn:
				g.AbortWithStatus(http.StatusUnauthorized)
			}
			return
		}

		logger := zerolog.Ctx(requestContext).With().Str(logging.MethodLogger, "authentication handler").Str(ConnectionIDKey, string(appConn.id)).Logger()

		// logger = logger.().Str(logging.MethodLogger, "connectHandler").Str(ConnectionIDKey, string(appConn.id)).Logger()

		wsConn, subsErr := websocket.Accept(g.Writer, g.Request, &websocket.AcceptOptions{
			OriginPatterns: []string{loadBalancerAddress},
		})
		if subsErr != nil {
			logger.Error().Msgf("failed to accept ws connection request: %v", subsErr)
			_ = g.Error(subsErr)
			return
		}

		var wsClosedError error
		defer func() {
			clientDisconnectCtx, clientDisconnectSpan := tracer.Start(requestContext, "new-ws-disconnect")
			defer clientDisconnectSpan.End()

			wsConn.Close(websocket.StatusNormalClosure, "")

			handleClientDisconnected(clientDisconnectCtx, appUrls, g.Request.Header, appConn, logger)

			if clusterSupport != nil {
				clusterSupport.deregisterConnection(requestContext, appConn.id)
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
			clusterSupport.registerConnection(requestContext, appConn.id)
		}

		if ackWithNewConnId {
			ackErr := sendMessageToClient(requestContext, wsConn, map[string]string{ConnectionIDKey: string(appConn.id)})
			if ackErr != nil {
				logger.Error().Err(fmt.Errorf("failed to send connect ack: %v", ackErr))
				wsClosedError = ackErr
				return
			}
		}

		logger.Debug().Msg("websocket message processing about to start...")

		wsClosedError = ws.processMessages(requestContext, appConn.id, &wsIOAdapter{wsConn}, handleClientMessage(appConn, appUrls)) // we block here until Error or Done

		logger.Debug().Msgf("websocket message processing finished with %v", wsClosedError)
	}
}

func pushHandler(ws *wsConnections, clusterSupport *ClusterSupport) gin.HandlerFunc {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.FunctionLogger, "pushHandler").Logger()
		logger.Debug().Msg("BEGIN")

		requestContext := monitoring.ExtractFromHeader(g.Request.Context(), g.Request.Header)
		tracer := otel.Tracer(config.OtelScope)
		tmpCtx, span := tracer.Start(requestContext, "push-message")
		defer span.End()
		span.AddEvent("push-request-received")
		requestContext = tmpCtx

		connectionIdStr := g.Param(connIdPathParamName)

		logger = logger.With().Str(ConnectionIDKey, connectionIdStr).Logger()

		if connectionIdStr == "" {
			logger.Info().Msgf("Missing path param: %s", connIdPathParamName)
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}

		logger.Debug().Msg("waiting for input on wsconn...")
		requestBody, errReadRequest := io.ReadAll(g.Request.Body)
		g.Request.Body.Close()
		if errReadRequest != nil {
			logger.Error().Msgf("failed to read request body %T: %v", g.Request.Body, errReadRequest)
			g.JSON(http.StatusInternalServerError, nil)
			return
		}
		logger.Debug().Msg("ws message received")

		bodyAsString := string(requestBody)

		span.AddEvent("pushing")

		errPush := ws.push(requestContext, bodyAsString, ConnectionID(connectionIdStr))
		if errPush == errConnectionNotFound {
			if clusterSupport == nil {
				logger.Info().Msg("ws connection not found")
				g.AbortWithStatus(http.StatusNotFound)
				return
			}
			logger.Info().Str("connectionIdStr", connectionIdStr).Msgf("ws connection '%s' isn't managed here, publishing payload...", connectionIdStr)
			errPush = clusterSupport.relayMessage(requestContext, ConnectionID(connectionIdStr), bodyAsString)
		}

		if errPush != nil {
			logger.Error().Msgf("Failed to push to connection %s: %v", connectionIdStr, errPush)
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		span.AddEvent("pushed")

		g.Status(http.StatusNoContent)

		logger.Debug().Msg("END")
	}
}

func cleanupResponse(response *http.Response) {
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
}
