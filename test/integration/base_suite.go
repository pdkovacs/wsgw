package integration

import (
	"context"
	"fmt"
	"sync"
	"wsgw/internal/config"
	"wsgw/test/mockapp"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	wsgw "wsgw/internal"
)

type baseTestSuite struct {
	suite.Suite
	wsgwerver       string
	ctx             context.Context
	wsGateway       *wsgw.Server
	mockApp         mockapp.MockApp
	connIdGenerator func() wsgw.ConnectionID
	// Fall-back connection-id in case no generator is specified to be used in strictly sequential test cases
	// testing in isolation the connection setup itself
	nextConnId wsgw.ConnectionID
}

func NewBaseTestSuite(ctx context.Context) *baseTestSuite {
	return &baseTestSuite{
		ctx: ctx,
	}
}

func (s *baseTestSuite) startMockApp() {
	s.mockApp = mockapp.NewMockApp(func() string {
		return fmt.Sprintf("http://%s", s.wsgwerver)
	})
	mockAppStartErr := s.mockApp.Start()
	if mockAppStartErr != nil {
		panic(mockAppStartErr)
	}
}

func (s *baseTestSuite) SetupSuite() {
	logger := zerolog.Ctx(s.ctx).With().Str("method", "SetupSuite").Logger()
	logger.Info().Msg("BEGIN")

	s.startMockApp()

	server := wsgw.NewServer(
		s.ctx,
		config.Config{
			ServerHost:          "localhost",
			ServerPort:          0,
			AppBaseUrl:          fmt.Sprintf("http://%s", s.mockApp.GetAppAddress()),
			LoadBalancerAddress: "",
		},
		func() wsgw.ConnectionID {
			if s.connIdGenerator == nil {
				return s.nextConnId
			}
			return s.connIdGenerator()
		},
	)
	s.wsGateway = server

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		logger.Debug().Msg("Setting up and starting the server...")
		err := server.SetupAndStart(func(port int, _ func()) {
			logger.Info().Msg("WsGateway is ready!")
			s.wsgwerver = fmt.Sprintf("localhost:%d", port)
			wg.Done()
		})
		logger.Warn().Err(err).Msg("error during server start")
	}()
	wg.Wait()
}

func (s *baseTestSuite) TearDownSuite() {
	if s.mockApp != nil {
		s.mockApp.Stop()
	}
	if s.wsGateway != nil {
		s.wsGateway.Stop()
	}
}

func (s *baseTestSuite) getCall(connId wsgw.ConnectionID, callIndex int) mock.Call {
	calls := s.mockApp.GetCalls(connId)
	return calls[callIndex]
}

func (s *baseTestSuite) assertArguments(call *mock.Call, objects ...interface{}) {
	call.Arguments.Assert(s.T(), objects...)
}

func toWsMessage(content string) mockapp.MessageJSON {
	return mockapp.MessageJSON{"message": content}
}
