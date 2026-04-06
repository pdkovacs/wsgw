package monitoring

import (
	"github.com/gin-gonic/gin"
)

func NewOtelTraceExtraction() func(g *gin.Context) {
	return func(g *gin.Context) {
		ctx := ExtractFromHeader(g.Request.Context(), g.Request.Header)
		g.Request = g.Request.WithContext(ctx)
		g.Next()
	}
}
