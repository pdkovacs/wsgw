package mockapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
)

const BadCredential = "bad-credential"

const (
	MockMethodConnect         = "connect"
	MockMethodDisconnected    = "disconnected"
	MockMethodMessageReceived = "messageReceived"
)

type MockApp interface {
	Start() error
	GetAppAddress() string
	Stop()
	SendToClient(connId wsgw.ConnectionID, message MessageJSON) error
	On(methodName string, connId wsgw.ConnectionID, arguments ...any)
	ExpectConnDisconn(connId wsgw.ConnectionID)
	GetCalls(connId wsgw.ConnectionID) []mock.Call
	OnDisconnect(connectionId wsgw.ConnectionID) chan struct{}
}

type MessageJSON map[string]string

type MyMock struct {
	disconnectNotification chan struct{}
	mock.Mock
}

func newClientPeer() *MyMock {
	return &MyMock{disconnectNotification: make(chan struct{})}
}

func (m *MyMock) connect() {
	m.Called()
}

func (m *MyMock) disconnected() {
	m.Called()
	m.disconnectNotification <- struct{}{}
}

func (m *MyMock) messageReceived(msg MessageJSON) {
	m.Called(msg)
}

type mockApplication struct {
	// getwsgwUrl makes available the URL of the WSGS server
	getwsgwUrl   func() string
	listener     net.Listener
	stop         func()
	logger       zerolog.Logger
	connMocks    map[string]*MyMock
	connMocksMux sync.Mutex
}

func NewMockApp(getwsgwUrl func() string) MockApp {
	return &mockApplication{
		getwsgwUrl: getwsgwUrl,
		logger:     logging.Get().With().Str("unit", "mockApplication").Logger(),
		connMocks:  make(map[string]*MyMock),
	}
}

func (m *mockApplication) Start() error {
	address := fmt.Sprintf(":%d", 0)
	listener, listenErr := net.Listen("tcp", address)
	if listenErr != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, listenErr)
	}
	m.listener = listener

	handler, creHandlerErr := m.createMockAppRequestHandler()
	if creHandlerErr != nil {
		return fmt.Errorf("failed to create mockApp request handler: %w", creHandlerErr)
	}

	go func() {
		serveErr := http.Serve(listener, handler)
		if serveErr != nil {
			m.logger.Error().Err(serveErr)
		}
	}()

	m.stop = func() {
		listener.Close()
	}

	return nil
}

func (m *mockApplication) GetAppAddress() string {
	return m.listener.Addr().String()
}

func (m *mockApplication) Stop() {
	m.stop()
}

func (m *mockApplication) createMockAppRequestHandler() (http.Handler, error) {
	rootEngine := gin.Default()
	rootEngine.Use(wsgw.RequestLogger("mockApplication"))

	ws := rootEngine.Group("/ws")

	ws.GET(string(wsgw.ConnectPath), func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str("method", "WS connect handler").Logger()
		req := g.Request
		res := g
		cred, hasCredHeader := req.Header["Authorization"]
		logger.Debug().Msgf("Request has authorization header: %v and it is: %s", hasCredHeader, cred)
		if !hasCredHeader {
			_ = res.AbortWithError(http.StatusInternalServerError, errors.New("authorization header not found"))
			return
		}
		if cred[0] == BadCredential {
			_ = res.AbortWithError(401, errors.New("bad credentials in Authorization header"))
			return
		}

		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			m.connMocksMux.Lock()
			defer m.connMocksMux.Unlock()

			_, ok := m.connMocks[connId]
			if !ok {
				logger.Info().Str(wsgw.ConnectionIDKey, connId).Msg("No mock for connection yet, creating...")
				m.connMocks[string(connId)] = newClientPeer()
				res.Status(http.StatusOK)
				return
			}
			m.connMocks[connId].connect()
		}

		res.Status(http.StatusOK)
	})

	ws.POST(string(wsgw.DisonnectedPath), func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str("method", "WS disconnection handler").Logger()
		req := g.Request
		res := g

		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			m.connMocksMux.Lock()
			defer m.connMocksMux.Unlock()
			if _, ok := m.connMocks[connId]; !ok {
				logger.Error().Str(wsgw.ConnectionIDKey, connId).Msg("connection not mocked")
				res.Status(http.StatusInternalServerError)
				return
			}
			m.connMocks[connId].disconnected()
		}
	})

	ws.POST(string(wsgw.MessagePath), func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str("method", "WS message handler").Logger()
		req := g.Request
		res := g

		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			bodyAsBytes, readBodyErr := io.ReadAll(req.Body)
			req.Body.Close()
			if readBodyErr != nil {
				logger.Error().Err(readBodyErr).Send()
				return
			}

			m.connMocksMux.Lock()
			defer m.connMocksMux.Unlock()
			if _, ok := m.connMocks[connId]; !ok {
				logger.Error().Str(wsgw.ConnectionIDKey, connId).Msg("connection not mocked")
				res.Status(http.StatusInternalServerError)
				return
			}
			m.connMocks[connId].messageReceived(parseMessageJSON(bodyAsBytes))
		}
	})

	return rootEngine, nil
}

func (m *mockApplication) OnDisconnect(connId wsgw.ConnectionID) chan struct{} {
	return m.connMocks[string(connId)].disconnectNotification
}

func (m *mockApplication) On(methodName string, connId wsgw.ConnectionID, arguments ...any) {
	m.connMocksMux.Lock()
	defer m.connMocksMux.Unlock()
	if _, ok := m.connMocks[string(connId)]; !ok {
		m.connMocks[string(connId)] = newClientPeer()
	}
	m.connMocks[string(connId)].On(methodName, arguments...)
}

func (s *mockApplication) ExpectConnDisconn(connId wsgw.ConnectionID) {
	s.On(MockMethodConnect, connId)
	s.On(MockMethodDisconnected, connId)
}

func (m *mockApplication) GetCalls(connId wsgw.ConnectionID) []mock.Call {
	m.connMocksMux.Lock()
	defer m.connMocksMux.Unlock()
	if _, ok := m.connMocks[string(connId)]; !ok {
		return []mock.Call{}
	}
	return m.connMocks[string(connId)].Calls
}

func (s *mockApplication) SendToClient(connId wsgw.ConnectionID, message MessageJSON) error {
	url := fmt.Sprintf("%s%s/%s", s.getwsgwUrl(), wsgw.MessagePath, connId)
	req, createReqErr := http.NewRequest(http.MethodPost, url, strings.NewReader(message["message"]))
	if createReqErr != nil {
		return createReqErr
	}
	client := http.Client{
		Timeout: time.Second * 15,
	}
	response, sendReqErr := client.Do(req)
	if sendReqErr != nil {
		return sendReqErr
	}

	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode)
	}

	return nil
}

func parseMessageJSON(value []byte) MessageJSON {
	message := map[string]string{}
	unmarshalErr := json.Unmarshal(value, &message)
	if unmarshalErr != nil {
		panic(unmarshalErr)
	}
	return message
}
