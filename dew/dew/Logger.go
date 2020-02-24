package dew

import (
	"log"
	"time"
)

func Logger() HandlerFunction {
	return func(context *Context) {
		start := time.Now()
		context.Next()
		log.Printf("[%d] %s in %v", context.Code, context.Request.RequestURI, time.Since(start))
	}
}
