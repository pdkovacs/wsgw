package wsgw

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"wsgw/internal/config"
	loadmanagement "wsgw/pkgs/loadmanegement"
	"wsgw/pkgs/monitoring"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"golang.org/x/net/http2"
)

// TODO: make this configurable?
const (
	ConnectionIDHeaderKey = "X-WSGW-CONNECTION-ID"
	ConnIdPathParamName   = ConnectionIDKey
	connIdPathParamName   = ConnIdPathParamName
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
	connecting(connId ConnectionID) string
	disconnected(connId ConnectionID) string
	message(connId ConnectionID) string
}

var httpClient http.Client = http.Client{
	Timeout: time.Second * 15,
}

func configureAppHTTPClient(http2Enabled bool) {
	if !http2Enabled {
		return
	}
	httpClient.Transport = &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext(ctx, network, addr)
		},
	}
}

// stripWSUpgradeHeaders clones the client's headers and removes the HTTP/1.1
// hop-by-hop WS upgrade headers, so the resulting set is safe to relay to the
// backend over HTTP/2 (which forbids them per RFC 7540 §8.1.2.2). The actual
// WS upgrade happens between the client and wsgw, not on the wsgw→backend leg.
func stripWSUpgradeHeaders(h http.Header) http.Header {
	cleaned := h.Clone()
	for _, name := range []string{"Upgrade", "Connection", "Sec-Websocket-Key", "Sec-Websocket-Version", "Sec-Websocket-Extensions", "Sec-Websocket-Protocol"} {
		cleaned.Del(name)
	}
	return cleaned
}

type appConnection struct {
	id         ConnectionID
	httpClient http.Client
}

var errAppConnInternal = errors.New("internalError")
var errAppConnAuthn = errors.New("authnError")
var errAppConnAccepting = errors.New("appError")

// Relays the connection request to the backend's `GET /ws/connect/{connectionId}` endpoint.
func handleClientConnecting(requestCtx context.Context, r *http.Request, createConnectionId func(ctx context.Context) ConnectionID, appUrls applicationURLs) (*appConnection, error) {
	logger := zerolog.Ctx(r.Context()).With().Logger()

	connId := createConnectionId(r.Context())
	urlPath := appUrls.connecting(connId)

	response, result, err := sendRequestToApp(requestCtx, http.MethodGet, urlPath, nil, r)
	if err != nil {
		return result, err
	}

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

func sendRequestToApp(requestCtx context.Context, method string, urlPath string, body *string, r *http.Request) (*http.Response, *appConnection, error) {
	logger := zerolog.Ctx(r.Context()).With().Logger()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(*body)
	}

	request, err := http.NewRequestWithContext(requestCtx, method, urlPath, bodyReader)
	if err != nil {
		logger.Error().Err(err).Msgf("failed to create request object")
		return nil, nil, errAppConnInternal
	}
	request.Header = stripWSUpgradeHeaders(r.Header)

	monitoring.InjectIntoHeader(requestCtx, request.Header)

	response, requestErr := httpClient.Do(request)
	if requestErr != nil {
		logger.Error().Err(requestErr).Msgf("failed to send request")
		return nil, nil, errAppConnInternal
	}
	defer cleanupResponse(response)
	return response, nil, nil
}

func handleClientDisconnected(ctx context.Context, appUrls applicationURLs, connReqHeader http.Header, appConn *appConnection, logger zerolog.Logger) {
	urlPath := appUrls.disconnected(appConn.id)
	logger = logger.With().Str("appUrl", urlPath).Str(ConnectionIDKey, string(appConn.id)).Logger()

	logger.Debug().Msg("BEGIN")

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, urlPath, nil)
	if err != nil {
		logger.Error().Err(err).Msgf("failed to create request object")
		return
	}
	request.Header = connReqHeader

	monitoring.InjectIntoHeader(ctx, request.Header)

	response, requestErr := appConn.httpClient.Do(request)
	if requestErr != nil {
		logger.Error().Err(requestErr).Msgf("failed to send request")
		return
	}
	defer cleanupResponse(response)

	if response.StatusCode != 200 {
		logger.Info().Msgf("Received status code %d", response.StatusCode)
		return
	}

	logger.Debug().Msg("END")
}

// Calls the `POST /ws/message/{connectionId}` endpoint on the backend with "msg" as the body.
func handleClientMessage(g *gin.Context, appConn *appConnection, messageUrl string) func(c context.Context, msg string) error {
	return func(c context.Context, msg string) error {
		logger := zerolog.Ctx(c).With().Str(ConnectionIDKey, string(appConn.id)).Str("func", "handleClientMessage").Logger()
		logger.Debug().Str("msg", msg).Send()

		response, _, requestErr := sendRequestToApp(c, http.MethodPost, messageUrl, &msg, g.Request)
		if requestErr != nil {
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

// connectHandler calls `authenticateClient` if it is not `nil` to authenticate the client,
// then notifies the application of the new WS connection
func connectHandler(
	appUrls applicationURLs,
	ws *wsConnections,
	loadBalancerAddress string,
	createConnectionId func(ctx context.Context) ConnectionID,
	ackWithNewConnId bool,
) gin.HandlerFunc {
	return func(g *gin.Context) {
		requestContext := g.Request.Context()
		tracer := otel.Tracer(config.OtelScope)
		requestContext, span := tracer.Start(requestContext, "new-ws-connection")
		defer span.End()

		appConn, clientConnectErr := handleClientConnecting(requestContext, g.Request, createConnectionId, appUrls)

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

		logger := zerolog.Ctx(requestContext).With().Str(ConnectionIDKey, string(appConn.id)).Logger()

		// logger = logger.().Str(logging.UnitLogger, "connectHandler").Str(ConnectionIDKey, string(appConn.id)).Logger()

		if ackWithNewConnId {
			g.Writer.Header().Set(ConnectionIDHeaderKey, string(appConn.id))
		}

		wsConn, subsErr := websocket.Accept(g.Writer, g.Request, &websocket.AcceptOptions{
			OriginPatterns: []string{loadBalancerAddress},
		})
		if subsErr != nil {
			logger.Error().Err(subsErr).Msgf("failed to accept ws connection request")
			_ = g.Error(subsErr)
			return
		}

		var wsClosedError error
		defer func() {
			clientDisconnectCtx, clientDisconnectSpan := tracer.Start(requestContext, "new-ws-disconnect")
			defer clientDisconnectSpan.End()

			wsConn.Close(websocket.StatusNormalClosure, "")

			handleClientDisconnected(clientDisconnectCtx, appUrls, stripWSUpgradeHeaders(g.Request.Header), appConn, logger)

			if wsClosedError != nil {
				if errors.Is(wsClosedError, context.Canceled) {
					return // Done
				}

				if websocket.CloseStatus(wsClosedError) == websocket.StatusNormalClosure ||
					websocket.CloseStatus(wsClosedError) == websocket.StatusGoingAway {
					return
				}

				logger.Error().Err(wsClosedError).Send()
			}
		}()

		logger.Debug().Msg("websocket message processing about to start...")

		wsClosedError = ws.processMessages(requestContext, appConn.id, &wsIOAdapter{wsConn}, handleClientMessage(g, appConn, appUrls.message(appConn.id))) // we block here until Error or Done

		logger.Debug().Msgf("websocket message processing finished with %v", wsClosedError)
	}
}

func pushHandler(ws *wsConnections) gin.HandlerFunc {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Logger()
		logger.Debug().Msg("BEGIN")

		requestContext := g.Request.Context()
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
			logger.Error().Err(errReadRequest).Msgf("failed to read request body %T", g.Request.Body)
			g.JSON(http.StatusInternalServerError, nil)
			return
		}
		logger.Debug().Msg("ws message received")

		bodyAsString := string(requestBody)

		span.AddEvent("pushing")

		errPush := ws.push(requestContext, bodyAsString, ConnectionID(connectionIdStr))
		var oload *loadmanagement.OverloadError
		if errors.As(errPush, &oload) {
			logger.Error().Err(errPush).Str("connectionIdStr", connectionIdStr).Msgf("failed to push to connection")
			g.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if errPush == errConnectionNotFound {
			logger.Info().Msg("ws connection not found")
			g.AbortWithStatus(http.StatusNotFound)
			return
		}
		if errPush != nil {
			logger.Error().Err(errPush).Str("connectionIdStr", connectionIdStr).Msgf("failed to push to connection")
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
