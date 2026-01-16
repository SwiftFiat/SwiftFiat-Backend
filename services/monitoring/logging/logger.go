package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/syslog"
	"os"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	logrusSyslog "github.com/sirupsen/logrus/hooks/syslog"
)

type Logger struct {
	*logrus.Logger
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r responseBodyWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func NewLogger() *Logger {
	c, err := utils.LoadConfig(utils.EnvPath)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)
	log.SetFormatter(&logrus.JSONFormatter{PrettyPrint: true})
	log.SetOutput(os.Stdout) // this enables logs to stdout (journalctl via vps)

	hook, err := logrusSyslog.NewSyslogHook("udp", c.Papertrail, syslog.LOG_INFO, c.PapertrailAppName)
	if err != nil {
		log.Error("Unable to connect to Papertrail")
	} else {
		log.Hooks.Add(hook)
	}

	return &Logger{
		log,
	}
}

func (l *Logger) LoggingMiddleWare() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Read the request body
		var requestBody []byte
		if c.Request.Body != nil {
			requestBody, _ = c.GetRawData()
			c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		// Check if this is a WebSocket upgrade request (case-insensitive)
		upgradeHeader := strings.ToLower(c.GetHeader("Upgrade"))
		connectionHeader := strings.ToLower(c.GetHeader("Connection"))
		isWebSocket := upgradeHeader == "websocket" || strings.Contains(connectionHeader, "upgrade")

		// Create a custom response writer to capture the response body
		// Skip for WebSockets as it interferes with the Hijack process
		var w *responseBodyWriter
		if !isWebSocket {
			w = &responseBodyWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
			c.Writer = w
		}

		// Process request
		c.Next()

		// Log after request is processed
		duration := time.Since(start)
		statusCode := c.Writer.Status()

		// For WebSockets, we don't capture the body
		if isWebSocket {
			fields := logrus.Fields{
				"method":   c.Request.Method,
				"path":     c.Request.URL.Path,
				"status":   statusCode,
				"duration": duration,
				"type":     "websocket",
			}
			l.WithFields(fields).Info("WebSocket Connection")
			return
		}

		var requestJson any
		var responseJson any
		err := json.Unmarshal(requestBody, &requestJson)
		if err != nil {
			l.Log(logrus.DebugLevel, "error unmarshalling requestBody, request may not be JSON")
		}

		err = json.Unmarshal(w.body.Bytes(), &responseJson)
		if err != nil {
			l.Log(logrus.DebugLevel, "error unmarshalling responseBody")
		}

		// var debug bool

		// mode := gin.Mode()
		// if mode == gin.DebugMode {
		// 	debug = true
		// } else {
		// 	debug = false
		// }

		fields := logrus.Fields{
			"method":   c.Request.Method,
			"path":     c.Request.URL.Path,
			"status":   statusCode,
			"duration": duration,
			// "response_body": responseJson,
		}

		// Only log request body if it's small to avoid polluting logs with large payloads
		// that could impact log storage and make debugging more difficult
		if len(requestBody) < 250 {
			fields["request"] = requestJson
		}

		// if debug {
		// 	fields["request_header"] = c.Request.Header
		// }

		l.WithFields(fields).Info("Request-Response")
	}
}
