package httpadapter

import (
	"context"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	wsgw "wsgw/internal"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/version_info"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/conntrack"
	"wsgw/test/e2e/app/internal/services"
	"wsgw/test/e2e/app/web"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/memstore"
	"github.com/gin-contrib/sessions/postgres"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

const BadCredential = "bad-credential"

const (
	MockMethodConnect         = "connect"
	MockMethodDisconnected    = "disconnected"
	MockMethodMessageReceived = "messageReceived"
)

type MessageJSON map[string]string

type server struct {
	server  *http.Server
	getwsgw func() string
}

func NewServer(getwsgw func() string) server {
	return server{
		getwsgw: getwsgw,
	}
}

// SetupAndStart sets up and starts server.
func (s *server) Start(serverCtx context.Context, conf config.Config, ready func(ctx context.Context, port int, stop func(ctx context.Context) error)) error {
	r := s.initEndpoints(serverCtx, conf)
	return s.start(serverCtx, conf.ServerPort, r, ready)
}

// Stop kills the listener
func (s *server) Stop(ctx context.Context) error {
	logger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "Stop").Logger()

	shutdownTimeout := 10 * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	logger.Debug().Dur("shutdownTimeout", shutdownTimeout).Msg("shutting server down")
	shutdownErr := s.server.Shutdown(shutdownCtx)
	logger.Debug().Msg("server shut down")
	if shutdownErr != nil {
		logger.Error().Err(shutdownErr).Msg("error while shutting down the server")
	} else {
		logger.Info().Msg("server shut successfully down")
	}
	return shutdownErr
}

func (s *server) initEndpoints(ctx context.Context, conf config.Config) *gin.Engine {
	logger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "server:initEndpoints").Logger()
	userService := services.NewUserService()

	var wsConnections, conntrackerErr = conntrack.NewWsgwConnectionTracker(ctx, conf.DynamodbURL)
	if conntrackerErr != nil {
		panic(fmt.Errorf("unable to create WSGW connection tracker %w", conntrackerErr))
	}

	rootEngine := gin.Default()
	rootEngine.Use(wsgw.RequestLogger("e2etest-application"))

	gob.Register(SessionData{})

	var connProps config.DbConnectionProperties
	if len(conf.DBHost) > 0 {
		connProps = config.CreateDbProperties(conf, logger)
	}

	var err error
	var store sessions.Store
	if store, err = s.createSessionStore(ctx, connProps, conf.SessionDB); err != nil {
		panic(fmt.Sprintf("failed to create session store: %v", err))
	}
	store.Options(sessions.Options{MaxAge: conf.SessionMaxAge})

	rootEngine.Use(sessions.Sessions("mysession", store))

	rootEngine.NoRoute(authentication(conf, &userService, zerolog.Ctx(ctx).With().Logger()), gin.WrapH(web.AssetHandler("/", "dist", logger)))

	rootEngine.GET("/app-info", func(c *gin.Context) {
		c.JSON(200, version_info.GetVersionInfo(config.GetVersionData()))
	})

	logger.Debug().Msg("Creating authorized group....")

	authorizedGroup := rootEngine.Group("/")
	{
		authorizedGroup.Use(authenticationCheck(conf, &userService))

		authorizedGroup.GET("/config", configHandler(conf.WsgwHost, conf.WsgwPort))
		authorizedGroup.GET("/user", userInfoHandler(userService))
		authorizedGroup.GET("/users", userListHandler(userService, conf.PasswordCredentials))

		apiGroup := authorizedGroup.Group("/api")
		apiHandler := newAPIHandler(config.GetWsgwUrl(conf), wsConnections)
		apiGroup.POST("/message", apiHandler.messageHandler())

		wsGroup := authorizedGroup.Group("/ws")
		wsHandler := newWSHandler(wsConnections)
		wsGroup.GET(string(wsgw.ConnectPath), wsHandler.connectWsHandler(wsConnections))
		wsGroup.POST(string(wsgw.DisonnectedPath), wsHandler.disconnectWsHandler(wsConnections))
		wsGroup.POST(string(wsgw.MessagePath), messageWsHandler())

		authorizedGroup.POST("/longrequest", func(g *gin.Context) {
			reqCtx := g.Request.Context()
			loggerReq := zerolog.Ctx(reqCtx)
			loggerReq.Info().Msg("about enter select")

			<-time.After(15 * time.Hour)
			loggerReq.Info().Msgf("request completed: %v", reqCtx.Err())
			g.JSON(200, gin.H{"status": "completed"})
			g.Status(200)
		})
	}

	return rootEngine
}

func (s *server) createSessionStore(ctx context.Context, connProps config.DbConnectionProperties, sessionDb string) (sessions.Store, error) {
	var store sessions.Store
	logger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "create-session properties").Logger()

	// I feel that we should have some implemenetation  connection URI for every (even app-specific for databases which don't have a widely available scheme)
	connScheme := strings.Split(sessionDb, ":")[0]
	switch connScheme {
	case "":
		logger.Info().Msg("Using in-memory session store")
		return memstore.NewStore([]byte("secret")), nil
	case "dynamodb":
		return nil, fmt.Errorf("unsupported session-db scheme: %s", connScheme)
	default:
		if len(connProps.Host) > 0 {
			connProps.Database = sessionDb
			logger.Info().Interface("sessionStore", connProps).Send()
			connStr := fmt.Sprintf(
				"postgres://%s:%s@%s:%d/%s?sslmode=disable",
				connProps.User,
				connProps.Password,
				connProps.Host,
				connProps.Port,
				connProps.Database,
			)
			sessionDb, openSessionDbErr := sql.Open("pgx", connStr)
			if openSessionDbErr != nil {
				return store, openSessionDbErr
			}
			sessionDb.Ping()
			var createDbSessionStoreErr error
			store, createDbSessionStoreErr = postgres.NewStore(sessionDb, []byte("secret"))
			if createDbSessionStoreErr != nil {
				return store, createDbSessionStoreErr
			}
		}
	}
	return store, nil
}

// start starts the service
func (s *server) start(serverCtx context.Context, portRequested int, r http.Handler, ready func(ctx context.Context, port int, stop func(ctx context.Context) error)) error {
	logger := zerolog.Ctx(serverCtx).With().Str(logging.MethodLogger, "StartServer").Logger()
	logger.Info().Msg("Starting listener....")

	lstnr, lstnrErr := net.Listen("tcp", fmt.Sprintf(":%d", portRequested))
	if lstnrErr != nil {
		panic(fmt.Sprintf("Error while starting listener at port: %v", lstnrErr))
	}

	_, port, err := net.SplitHostPort(lstnr.Addr().String())
	if err != nil {
		panic(fmt.Sprintf("Error while parsing the server address: %v", err))
	}

	logger.Info().Str("port", port).Msg("started to listen")

	if ready != nil {
		portAsInt, err := strconv.Atoi(port)
		if err != nil {
			panic(err)
		}
		ready(serverCtx, portAsInt, s.Stop)
	}

	s.server = &http.Server{
		BaseContext:  func(l net.Listener) context.Context { return serverCtx },
		Handler:      r,
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	logger.Info().Msg("starting to serve...")
	srvErr := s.server.Serve(lstnr)
	logger.Info().Msgf("serve returned %#v", srvErr)
	return srvErr
}

func parseMessageJSON(value []byte) MessageJSON {
	message := map[string]string{}
	unmarshalErr := json.Unmarshal(value, &message)
	if unmarshalErr != nil {
		panic(unmarshalErr)
	}
	return message
}
