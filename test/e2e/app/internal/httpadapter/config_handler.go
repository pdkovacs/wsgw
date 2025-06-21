package httpadapter

import "github.com/gin-gonic/gin"

type clientConfig struct {
	WsgwHost string `json:"wsgwHost"`
	WsgwPort int    `json:"wsgwPort"`
}

func configHandler(wsgwHost string, wsgwPort int) func(c *gin.Context) {
	return func(c *gin.Context) {
		c.JSON(200, clientConfig{wsgwHost, wsgwPort})
	}
}
