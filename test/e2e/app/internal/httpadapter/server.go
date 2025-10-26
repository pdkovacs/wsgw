package httpadapter

import (
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"
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
	listener      net.Listener
	configuration config.Options
	getwsgw       func() string
	logger        zerolog.Logger
}

func NewServer(configuration config.Options, getwsgw func() string) server {
	return server{
		configuration: configuration,
		getwsgw:       getwsgw,
		logger:        logging.Get().With().Str(logging.UnitLogger, "http-server").Logger(),
	}
}

// start starts the service
func (s *server) start(portRequested int, r http.Handler, ready func(port int, stop func())) {
	logger := s.logger.With().Str(logging.MethodLogger, "StartServer").Logger()
	logger.Info().Msg("Starting server on ephemeral....")
	var err error

	s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", portRequested))
	if err != nil {
		panic(fmt.Sprintf("Error while starting to listen at an ephemeral port: %v", err))
	}

	_, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		panic(fmt.Sprintf("Error while parsing the server address: %v", err))
	}

	logger.Info().Str("port", port).Msg("started to listen")

	if ready != nil {
		portAsInt, err := strconv.Atoi(port)
		if err != nil {
			panic(err)
		}
		ready(portAsInt, s.Stop)
	}

	http.Serve(s.listener, r)
}

// SetupAndStart sets up and starts server.
func (s *server) Start(options config.Options, ready func(port int, stop func())) {
	r := s.initEndpoints(options)
	s.start(options.ServerPort, r, ready)
}

func (s *server) initEndpoints(options config.Options) *gin.Engine {
	logger := s.logger.With().Str(logging.MethodLogger, "server:initEndpoints").Logger()
	userService := services.NewUserService()

	var wsConnections, conntrackerErr = conntrack.NewWsgwConnectionTracker(options.DynamodbURL)
	if conntrackerErr != nil {
		panic(fmt.Errorf("unable to create WSGW connection tracker %w", conntrackerErr))
	}

	rootEngine := gin.Default()
	rootEngine.Use(wsgw.RequestLogger("e2etest-application"))

	gob.Register(SessionData{})
	store, createStoreErr := s.createSessionStore(options)
	if createStoreErr != nil {
		panic(createStoreErr)
	}
	store.Options(sessions.Options{MaxAge: options.SessionMaxAge})
	rootEngine.Use(sessions.Sessions("mysession", store))

	rootEngine.NoRoute(authentication(options, &userService, s.logger.With().Logger()), gin.WrapH(web.AssetHandler("/", "dist", logger)))

	rootEngine.GET("/app-info", func(c *gin.Context) {
		c.JSON(200, config.GetBuildInfo())
	})

	logger.Debug().Msg("Creating authorized group....")

	authorizedGroup := rootEngine.Group("/")
	{
		authorizedGroup.Use(authenticationCheck(options, &userService))

		logger.Debug().Msg("Setting up logout handler")

		authorizedGroup.GET("/config", configHandler(options.WsgwHost, options.WsgwPort))
		authorizedGroup.GET("/user", userInfoHandler(userService))
		authorizedGroup.GET("/users", userListHandler(userService, options.PasswordCredentials))

		apiGroup := authorizedGroup.Group("/api")
		apiGroup.POST("/message", messageHandler(fmt.Sprintf("http://%s:%d", options.WsgwHost, options.WsgwPort), wsConnections))

		wsGroup := authorizedGroup.Group("/ws")
		wsGroup.GET(string(wsgw.ConnectPath), connectWsHandler(wsConnections))
		wsGroup.POST(string(wsgw.DisonnectedPath), disconnectWsHandler(wsConnections))
		wsGroup.POST(string(wsgw.MessagePath), messageWsHanlder())
	}

	return rootEngine
}

func (s *server) createSessionStore(options config.Options) (sessions.Store, error) {
	var store sessions.Store
	logger := s.logger.With().Str(logging.MethodLogger, "create-session properties").Logger()

	if options.SessionDbName == "" {
		logger.Info().Msg("Using in-memory session store")
		store = memstore.NewStore([]byte("secret"))
	} else if len(options.DynamodbURL) > 0 {
		panic("DynamoDB session store is not supported yet")
	} else if len(options.DBHost) > 0 {
		logger.Info().Str("database", options.SessionDbName).Msg("connecting to session store")
		connProps := config.CreateDbProperties(s.configuration, logger)
		connStr := fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			connProps.User,
			connProps.Password,
			connProps.Host,
			connProps.Port,
			options.SessionDbName,
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

	return store, nil
}

// Stop kills the listener
func (s *server) Stop() {
	logger := s.logger.With().Str(logging.MethodLogger, "ListenerKiller").Logger()
	error := s.listener.Close()
	if error != nil {
		logger.Error().Err(error).Interface("listener", s.listener).Msg("Error while closing listener")
	} else {
		logger.Info().Interface("listener", s.listener).Msg("Listener closed successfully")
	}
}

func parseMessageJSON(value []byte) MessageJSON {
	message := map[string]string{}
	unmarshalErr := json.Unmarshal(value, &message)
	if unmarshalErr != nil {
		panic(unmarshalErr)
	}
	return message
}
