package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func requestBodyLimitMiddleware(limitBytes int64) gin.HandlerFunc {
	if limitBytes <= 0 {
		limitBytes = 16 << 20
	}

	return func(c *gin.Context) {
		if c.Request != nil && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limitBytes)
		}
		c.Next()
	}
}
