package integration

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"wsgw/internal/config"
	"wsgw/pkgs/logging"
	"wsgw/test/mockapp"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	wsgw "wsgw/internal"
)

type baseTestSuite struct {
	suite.Suite
	wsgwerver       string
	ctx             context.Context
	cancel          context.CancelFunc
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
	mockAppStartErr := s.mockApp.Start(s.ctx)
	if mockAppStartErr != nil {
		panic(mockAppStartErr)
	}
}

func (s *baseTestSuite) SetupSuite() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	s.cancel = stop

	logger := logging.Get().With().Str(logging.MethodLogger, "SetupSuite").Logger()
	s.ctx = logger.WithContext(ctx)

	logger.Info().Msg("BEGIN")

	s.startMockApp()

	configuration := config.Config{
		ServerHost:           "localhost",
		ServerPort:           0,
		AppBaseUrl:           fmt.Sprintf("http://%s", s.mockApp.GetAppAddress()),
		AckNewConnWithConnId: true,
		LoadBalancerAddress:  "",
	}

	server := wsgw.NewServer(
		configuration,
		func(_ context.Context) wsgw.ConnectionID {
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
		err := server.SetupAndStart(
			s.ctx,
			configuration,
			func(ctx context.Context, port int, _ func(ctx context.Context) error) {
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
		s.mockApp.Stop(s.ctx)
	}
	if s.wsGateway != nil {
		s.wsGateway.Stop(s.ctx)
	}
	s.cancel()
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
