package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
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

	// Add Loki integration if configured
	if c.LokiURL != "" {
		lokiHook, err := NewLokiHook(c.LokiURL, c.Env)
		if err != nil {
			log.Error("Unable to connect to Loki: " + err.Error())
		} else {
			log.Hooks.Add(lokiHook)
		}
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

// LokiHook implements logrus hook interface for sending logs to Loki
type LokiHook struct {
	lokiURL string
	env     string
}

// NewLokiHook creates a new Loki hook
func NewLokiHook(lokiURL, env string) (*LokiHook, error) {
	return &LokiHook{
		lokiURL: lokiURL,
		env:     env,
	}, nil
}

// Levels defines the log levels this hook will be triggered for
func (h *LokiHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire is called when a log event is fired
func (h *LokiHook) Fire(entry *logrus.Entry) error {
	logLine := fmt.Sprintf("[%s] %s: %s", entry.Level.String(), entry.Time.Format(time.RFC3339), entry.Message)

	// Add entry fields to the log line
	fieldsStr := ""
	for k, v := range entry.Data {
		fieldsStr += fmt.Sprintf(" %s=%v", k, v)
	}
	logLine += fieldsStr

	// Build label map with environment and log level
	labels := map[string]string{
		"job":   "swiftfiat-api",
		"env":   h.env,
		"level": entry.Level.String(),
	}

	// Build stream for Loki
	stream := map[string]interface{}{
		"stream": labels,
		"values": [][]string{
			{
				fmt.Sprintf("%d", entry.Time.UnixNano()),
				logLine,
			},
		},
	}

	// Build request payload
	payload := map[string]interface{}{
		"streams": []interface{}{stream},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Send to Loki asynchronously (non-blocking, errors are logged but don't fail the application)
	go sendToLoki(h.lokiURL, jsonData)

	return nil
}

// sendToLoki sends logs to Loki asynchronously via HTTP
func sendToLoki(lokiURL string, payload []byte) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("POST", lokiURL, bytes.NewBuffer(payload))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// Read response to ensure proper cleanup
	io.ReadAll(resp.Body)
}
