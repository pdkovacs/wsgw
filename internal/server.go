package wsgw

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
	"wsgw/internal/config"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/version_info"

	"github.com/gin-gonic/gin"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
)

type EndpointPath string

const (
	ConnectPath     EndpointPath = "/connect"
	DisonnectedPath EndpointPath = "/disconnected"
	MessagePath     EndpointPath = "/message"
)

type Server struct {
	server             http.Server
	createConnectionId func(ctx context.Context) ConnectionID
	clusterSupport     *ClusterSupport
}

func NewServer(
	configuration config.Config,
	createConnectionId func(ctx context.Context) ConnectionID,
) *Server {
	return &Server{
		createConnectionId: createConnectionId,
		clusterSupport:     NewClusterSupport(configuration),
	}
}

// SetupAndStart sets up and starts server.
func (s *Server) SetupAndStart(serverCtx context.Context, configuration config.Config, ready func(ctx context.Context, port int, stop func(ctx context.Context) error)) error {
	r := createWsgwRequestHandler(configuration, s.createConnectionId, s.clusterSupport)
	return s.start(serverCtx, configuration, r, ready)
}

// For now, we assume that the backend authentication is managed ex-machina by the environment (AWS role or K8S NetworkPolicy
// or by a service-mesh provider)
// In the unlikely case of ex-machina control isn't available, OAuth2 client credentials flow could be easily supported.
// (Use https://pkg.go.dev/github.com/golang-jwt/jwt/v4#example-package-GetTokenViaHTTP to verify the token.)
func authenticateBackend(_ *gin.Context) error {
	return nil
}

// start starts the service
func (s *Server) start(serverCtx context.Context, configuration config.Config, r http.Handler, ready func(ctx context.Context, port int, stop func(ctx context.Context) error)) error {
	logger := zerolog.Ctx(serverCtx).With().Str(logging.MethodLogger, "start").Logger()

	endpoint := fmt.Sprintf("%s:%d", configuration.ServerHost, configuration.ServerPort)
	listener, err := net.Listen("tcp", endpoint)
	if err != nil {
		panic(fmt.Sprintf("Error while starting to listen at: %s", endpoint))
	}
	logger.Info().Msgf("wsgw instance is listening at %s", listener.Addr().String())

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		panic(fmt.Sprintf("Error while parsing the server address: %v", err))
	}

	logger.Info().Msgf("Listening on port: %s", port)

	if ready != nil {
		portAsInt, err := strconv.Atoi(port)
		if err != nil {
			panic(err)
		}
		ready(serverCtx, portAsInt, s.Stop)
	}

	server := &http.Server{
		BaseContext:  func(l net.Listener) context.Context { return serverCtx },
		Handler:      r,
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  90 * time.Second,
	}

	return server.Serve(listener)
}

// Stop kills the listener
func (s *Server) Stop(ctx context.Context) error {
	logger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "stop").Logger()
	logger.Info().Msgf("Shutting down server...")
	shutdownErr := s.server.Shutdown(ctx)
	if shutdownErr != nil {
		logger.Error().Err(shutdownErr).Msgf("Error while shutting down server")
	} else {
		logger.Info().Msg("Server shutdown successfully")
	}
	return shutdownErr
}

func createWsgwRequestHandler(configuration config.Config, createConnectionId func(ctx context.Context) ConnectionID, clusterSupport *ClusterSupport) *gin.Engine {
	rootEngine := gin.Default()

	rootEngine.Use(RequestLogger("websocketGatewayServer"))

	rootEngine.GET("/app-info", func(c *gin.Context) {
		c.JSON(200, version_info.GetVersionInfo(config.GetVersionData()))
	})

	wsConns := newWsConnections()

	appUrls := appURLs{
		baseUrl: configuration.AppBaseUrl,
	}

	rootEngine.GET(
		string(ConnectPath),
		connectHandler(
			&appUrls,
			wsConns,
			configuration.LoadBalancerAddress,
			createConnectionId,
			configuration.AckNewConnWithConnId,
			clusterSupport,
		),
	)

	rootEngine.POST(
		fmt.Sprintf("/message/:%s", connIdPathParamName),
		pushHandler(
			wsConns,
			clusterSupport,
		),
	)

	return rootEngine
}

type appURLs struct {
	baseUrl string
}

func (u *appURLs) connecting() string {
	return fmt.Sprintf("%s/ws%s", u.baseUrl, ConnectPath)
}

func (u *appURLs) disconnected() string {
	return fmt.Sprintf("%s/ws%s", u.baseUrl, DisonnectedPath)
}

func (u *appURLs) message() string {
	return fmt.Sprintf("%s/ws%s", u.baseUrl, MessagePath)
}

func RequestLogger(unitName string) func(g *gin.Context) {
	return func(g *gin.Context) {
		start := time.Now()

		r := g.Request
		l := logging.Get().With().
			Str("req_xid", xid.New().String()).
			Str("req_method", g.Request.Method).
			Str("req_url", g.Request.URL.RequestURI()).
			Str("req_remote_addr", r.RemoteAddr).
			Logger()
		l.Debug().Str("unit", unitName).
			Str("user_agent", g.Request.UserAgent()).
			Msg("incoming request starting")
		g.Request = r.WithContext(l.WithContext(r.Context()))

		defer func() {
			statusCode := g.Writer.Status()

			panicVal := recover()
			if panicVal != nil {
				statusCode = http.StatusInternalServerError // ensure that the status code is updated
				panic(panicVal)                             // continue panicking
			}

			l.Info().
				Int("status_code", statusCode).
				Dur("elapsed_ms", time.Since(start)).
				Msg("incoming request finished")
		}()

		g.Next()
	}
}
