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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	DebugLevel = zapcore.DebugLevel
	InfoLevel  = zapcore.InfoLevel
	WarnLevel  = zapcore.WarnLevel
	ErrorLevel = zapcore.ErrorLevel
	PanicLevel = zapcore.PanicLevel
	FatalLevel = zapcore.FatalLevel
)

type Fields map[string]any

type Logger struct {
	*zap.SugaredLogger
	lokiURL string
	env     string
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

	// Configure zap encoder
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.StacktraceKey = "" // Disable stacktrace for cleaner logs

	// Create cores
	consoleCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zap.DebugLevel,
	)

	var core zapcore.Core
	if c.LokiURL != "" {
		lokiCore := &LokiCore{
			lokiURL:      c.LokiURL,
			env:          c.Env,
			LevelEnabler: zap.DebugLevel,
			enc:          zapcore.NewJSONEncoder(encoderConfig),
		}
		core = zapcore.NewTee(consoleCore, lokiCore)
	} else {
		core = consoleCore
	}

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

	return &Logger{
		SugaredLogger: logger.Sugar(),
		lokiURL:       c.LokiURL,
		env:           c.Env,
	}
}

// WithFields provides compatibility with logrus.Fields
func (l *Logger) WithFields(fields Fields) *Logger {
	f := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		f = append(f, k, v)
	}
	return &Logger{
		SugaredLogger: l.SugaredLogger.With(f...),
		lokiURL:       l.lokiURL,
		env:           l.env,
	}
}

// Log provides compatibility with logrus.Log
func (l *Logger) Log(level zapcore.Level, args ...any) {
	switch level {
	case zapcore.DebugLevel:
		l.Debug(args...)
	case zapcore.InfoLevel:
		l.Info(args...)
	case zapcore.WarnLevel:
		l.Warn(args...)
	case zapcore.ErrorLevel:
		l.Error(args...)
	case zapcore.DPanicLevel:
		l.DPanic(args...)
	case zapcore.PanicLevel:
		l.Panic(args...)
	case zapcore.FatalLevel:
		l.Fatal(args...)
	default:
		l.Info(args...)
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
			l.Infow("WebSocket Connection",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"status", statusCode,
				"duration", duration,
				"type", "websocket",
			)
			return
		}

		var requestJson any
		var responseJson any
		_ = json.Unmarshal(requestBody, &requestJson)
		_ = json.Unmarshal(w.body.Bytes(), &responseJson)

		// Prepare fields for structured logging
		fields := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", statusCode,
			"duration", duration,
		}

		// Only log request body if it's small
		if len(requestBody) > 0 && len(requestBody) < 250 {
			fields = append(fields, "request", requestJson)
		}

		l.Infow("Request-Response", fields...)
	}
}

// LokiCore implements zapcore.Core for sending logs to Loki
type LokiCore struct {
	zapcore.LevelEnabler
	lokiURL string
	env     string
	enc     zapcore.Encoder
}

func (c *LokiCore) With(fields []zapcore.Field) zapcore.Core {
	clone := *c
	clone.enc = c.enc.Clone()
	for _, f := range fields {
		f.AddTo(clone.enc)
	}
	return &clone
}

func (c *LokiCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *LokiCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	buf, err := c.enc.EncodeEntry(ent, fields)
	if err != nil {
		return err
	}
	logLine := buf.String()
	buf.Free()

	// Build label map
	labels := map[string]string{
		"job":   "swiftfiat-api",
		"env":   c.env,
		"level": ent.Level.String(),
	}

	// Build stream for Loki
	stream := map[string]any{
		"stream": labels,
		"values": [][]string{
			{
				fmt.Sprintf("%d", ent.Time.UnixNano()),
				logLine,
			},
		},
	}

	// Build request payload
	payload := map[string]any{
		"streams": []any{stream},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Send to Loki asynchronously
	go sendToLoki(c.lokiURL, jsonData)

	return nil
}

func (c *LokiCore) Sync() error {
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
	_, _ = io.ReadAll(resp.Body)
}
