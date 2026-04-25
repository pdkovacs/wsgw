package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	wsgw "wsgw/internal"
	"wsgw/internal/config"
	"wsgw/pkgs/logging"
	"wsgw/test/mockapp"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// numClusterInstances is the cluster size used by the cluster integration suite.
// Three is the smallest size where the relay path is non-trivial: any one client
// has at least two non-owner peers to be reached via.
const numClusterInstances = 3

type clusterBaseTestSuite struct {
	suite.Suite
	wsgwervers      []string
	wsGateways      []*wsgw.Server
	ctx             context.Context
	cancel          context.CancelFunc
	mockApp         mockapp.MockApp
	valkey          *miniredis.Miniredis
	connIdGenerator func() wsgw.ConnectionID
	nextConnId      wsgw.ConnectionID
}

func NewClusterBaseTestSuite(ctx context.Context) *clusterBaseTestSuite {
	return &clusterBaseTestSuite{ctx: ctx}
}

func (s *clusterBaseTestSuite) startMockApp() {
	s.mockApp = mockapp.NewMockApp(func() string {
		return fmt.Sprintf("http://%s", s.wsgwervers[0])
	})
	if err := s.mockApp.Start(s.ctx); err != nil {
		panic(err)
	}
}

func (s *clusterBaseTestSuite) SetupSuite() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	s.cancel = stop

	logger := logging.Get().With().Logger()
	s.ctx = logger.WithContext(ctx)
	logger.Info().Msg("BEGIN cluster suite setup")

	v, err := miniredis.Run()
	if err != nil {
		panic(fmt.Sprintf("miniredis start: %v", err))
	}
	s.valkey = v

	// Pre-allocate ports so each instance knows its own InstanceAddr before binding.
	// We open a listener on :0, read the port, then close it — wsgw rebinds in a
	// separate Listen call. The race window between close and rebind is tiny; if it
	// ever causes flakes we can refactor server.go to accept a pre-opened listener.
	ports := make([]int, numClusterInstances)
	for i := 0; i < numClusterInstances; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(fmt.Sprintf("port pre-allocate: %v", err))
		}
		_, portStr, _ := net.SplitHostPort(l.Addr().String())
		port, _ := strconv.Atoi(portStr)
		ports[i] = port
		l.Close()
	}

	s.wsgwervers = make([]string, numClusterInstances)
	for i, port := range ports {
		s.wsgwervers[i] = fmt.Sprintf("127.0.0.1:%d", port)
	}

	s.startMockApp()

	s.wsGateways = make([]*wsgw.Server, numClusterInstances)
	var wg sync.WaitGroup
	wg.Add(numClusterInstances)
	for i := 0; i < numClusterInstances; i++ {
		i := i
		configuration := config.Config{
			ServerHost:           "127.0.0.1",
			ServerPort:           ports[i],
			AppBaseUrl:           fmt.Sprintf("http://%s", s.mockApp.GetAppAddress()),
			AckNewConnWithConnId: true,
			LoadBalancerAddress:  "",
			InstanceAddr:         s.wsgwervers[i],
			ValkeyURL:            fmt.Sprintf("redis://%s", s.valkey.Addr()),
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
		s.wsGateways[i] = server

		go func() {
			err := server.SetupAndStart(
				s.ctx,
				configuration,
				func(_ context.Context, _ int, _ func(ctx context.Context) error) {
					logger.Info().Int("instance", i).Str("addr", s.wsgwervers[i]).Msg("wsgw instance ready")
					wg.Done()
				},
			)
			logger.Warn().Err(err).Int("instance", i).Msg("wsgw instance exited")
		}()
	}
	wg.Wait()
}

func (s *clusterBaseTestSuite) TearDownSuite() {
	if s.mockApp != nil {
		s.mockApp.Stop(s.ctx)
	}
	for _, srv := range s.wsGateways {
		if srv != nil {
			srv.Stop(s.ctx)
		}
	}
	if s.valkey != nil {
		s.valkey.Close()
	}
	s.cancel()
}

// instanceUrl returns the http base URL of the i-th wsgw instance.
func (s *clusterBaseTestSuite) instanceUrl(i int) string {
	return fmt.Sprintf("http://%s", s.wsgwervers[i])
}

// pickPeer returns an instance index that is guaranteed to differ from `owner`.
// The returned index is deterministic given the inputs to keep test failures reproducible.
func (s *clusterBaseTestSuite) pickPeer(owner int, salt int) int {
	// Mod by (N-1) so the offset is in [1, N-1], then wrap around — this guarantees
	// the result is in [0, N) \ {owner}.
	return (owner + 1 + (salt % (numClusterInstances - 1))) % numClusterInstances
}

func (s *clusterBaseTestSuite) getCall(connId wsgw.ConnectionID, callIndex int) mock.Call {
	calls := s.mockApp.GetCalls(connId)
	return calls[callIndex]
}

func (s *clusterBaseTestSuite) assertArguments(call *mock.Call, objects ...interface{}) {
	call.Arguments.Assert(s.T(), objects...)
}
