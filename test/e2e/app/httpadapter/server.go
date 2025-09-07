package httpadapter

import (
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	wsgw "wsgw/internal"
	"wsgw/internal/app_errors"
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/config"
	"wsgw/test/e2e/app/security/authn"
	"wsgw/test/e2e/app/services"
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
	authorizationService := services.NewAuthorizationService(options)
	userService := services.NewUserService(&authorizationService)

	rootEngine := gin.Default()
	rootEngine.Use(wsgw.RequestLogger("e2etest-application"))

	if options.AuthenticationType != authn.SchemeOIDCProxy {
		gob.Register(SessionData{})
		store, createStoreErr := s.createSessionStore(options)
		if createStoreErr != nil {
			panic(createStoreErr)
		}
		store.Options(sessions.Options{MaxAge: options.SessionMaxAge})
		rootEngine.Use(sessions.Sessions("mysession", store))
	}

	rootEngine.NoRoute(authentication(options, &userService, s.logger.With().Logger()), gin.WrapH(web.AssetHandler("/", "dist", logger)))

	logger.Debug().Str("authenticationType", string(options.AuthenticationType)).Msg("Creating login end-point...")
	rootEngine.GET("/login", authentication(options, &userService, s.logger.With().Logger()))

	rootEngine.GET("/app-info", func(c *gin.Context) {
		c.JSON(200, config.GetBuildInfo())
	})

	logger.Debug().Msg("Creating authorized group....")

	// mustGetUserInfo := func(c *gin.Context) authr.UserInfo {
	// 	userInfo, getUserInfoErr := getUserInfo(options.AuthenticationType)(c)
	// 	if getUserInfoErr != nil {
	// 		panic(fmt.Sprintf("failed to get user-info %s", c.Request.URL))
	// 	}
	// 	return userInfo
	// }

	authorizedGroup := rootEngine.Group("/")
	{
		logger.Debug().Str("authn-type", string(options.AuthenticationType)).Msg("Setting up authorized group")
		authorizedGroup.Use(authenticationCheck(options, &userService))

		rootEngine.GET("/config", func(c *gin.Context) {
			c.JSON(200, clientConfig{IdPLogoutURL: options.OIDCLogoutURL})
		})
		logger.Debug().Msg("Setting up logout handler")
		authorizedGroup.POST("/logout", logout(options))

		authorizedGroup.GET("/user", userInfoHandler(options.AuthenticationType, userService))

		authorizedGroup.GET("/users", userListHandler(userService))

		if options.EnableBackdoors {
			authorizedGroup.PUT("/backdoor/authentication", HandlePutIntoBackdoorRequest())
			authorizedGroup.GET("/backdoor/authentication", HandleGetIntoBackdoorRequest())
		}

		apiGroup := authorizedGroup.Group("/api")
		{
			apiGroup.POST("/message", func(g *gin.Context) {
				logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "WS connect handler").Logger()

				body, errReadBody := io.ReadAll(g.Request.Body)
				if errReadBody != nil {
					logger.Error().Err(errReadBody).Msg("failed to read request body")
					g.AbortWithStatus(http.StatusBadRequest)
					return
				}
				var messageIn services.Message
				errMessageUnmarshal := json.Unmarshal(body, &messageIn)
				if errMessageUnmarshal != nil {
					logger.Error().Err(errMessageUnmarshal).Msg("failed to unmarshal request body into services.Message")
					g.AbortWithStatus(http.StatusBadRequest)
					return
				}

				errProcessMessage := services.ProcessMessage(g.Request.Context(), messageIn, userService)
				if errProcessMessage != nil {
					logger.Error().Err(errProcessMessage).Msg("failed to process message")
					var badRequest *app_errors.BadRequest
					if errors.As(errProcessMessage, &badRequest) {
						g.AbortWithStatusJSON(http.StatusBadRequest, badRequest.Error())
						return
					}
					g.AbortWithStatus(http.StatusInternalServerError)
					return
				}

				g.Status(http.StatusOK)
			})
		}

		wsGroup := authorizedGroup.Group("/ws")
		{
			wsGroup.GET(string(wsgw.ConnectPath), func(g *gin.Context) {
				logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "WS connect handler").Logger()
				req := g.Request
				res := g

				connHeaderKey := wsgw.ConnectionIDHeaderKey
				if connId := req.Header.Get(connHeaderKey); connId != "" {
					logger.Info().Str(wsgw.ConnectionIDKey, connId).Str("connid", connId).Msg("Incoming connection request...")
					res.Status(http.StatusOK)
					return
				}

				res.Status(http.StatusOK)
			})

			wsGroup.POST(string(wsgw.DisonnectedPath), func(g *gin.Context) {
				logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "WS disconnection handler").Logger()
				req := g.Request
				res := g

				connHeaderKey := wsgw.ConnectionIDHeaderKey
				if connId := req.Header.Get(connHeaderKey); connId != "" {
					logger.Error().Str(wsgw.ConnectionIDKey, connId).Str("connid", connId).Msg("Disconnect requested")
					res.Status(http.StatusOK)
					return
				}
			})

			wsGroup.POST(string(wsgw.MessagePath), func(g *gin.Context) {
				logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "WS message handler").Logger()
				req := g.Request
				connHeaderKey := wsgw.ConnectionIDHeaderKey
				if connId := req.Header.Get(connHeaderKey); connId != "" {
					bodyAsBytes, readBodyErr := io.ReadAll(req.Body)
					req.Body.Close()
					if readBodyErr != nil {
						logger.Error().Err(readBodyErr).Send()
						return
					}

					message := parseMessageJSON(bodyAsBytes)
					logger.Debug().Str(wsgw.ConnectionIDKey, connId).Any("message", message).Msg("message received")
				}
			})
		}
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

func (s *server) SendToClient(connId wsgw.ConnectionID, message MessageJSON) error {
	url := fmt.Sprintf("%s%s/%s", s.getwsgw(), wsgw.MessagePath, connId)
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
