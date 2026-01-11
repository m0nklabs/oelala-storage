// Package logging provides structured logging capabilities using Zap.
package logging

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// Logger is the global logger instance
	Logger *zap.Logger
	// Sugar is the sugared logger for printf-style logging
	Sugar *zap.SugaredLogger
)

// Config holds logging configuration
type Config struct {
	Level       string `mapstructure:"level"`
	Development bool   `mapstructure:"development"`
	Encoding    string `mapstructure:"encoding"` // json or console
	OutputPath  string `mapstructure:"output_path"`
}

// DefaultConfig returns default logging config
func DefaultConfig() Config {
	return Config{
		Level:       "info",
		Development: false,
		Encoding:    "json",
		OutputPath:  "stdout",
	}
}

// Init initializes the global logger
func Init(cfg Config) error {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}

	var zapCfg zap.Config
	if cfg.Development {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}

	zapCfg.Level = zap.NewAtomicLevelAt(level)
	zapCfg.Encoding = cfg.Encoding

	if cfg.OutputPath != "" && cfg.OutputPath != "stdout" {
		zapCfg.OutputPaths = []string{cfg.OutputPath}
	}

	Logger, err = zapCfg.Build(
		zap.AddCallerSkip(1),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	if err != nil {
		return err
	}

	Sugar = Logger.Sugar()
	return nil
}

// InitDefault initializes with default settings
func InitDefault() {
	if Logger != nil {
		return
	}

	var err error
	if os.Getenv("ENV") == "development" || os.Getenv("DEBUG") == "1" {
		Logger, err = zap.NewDevelopment()
	} else {
		Logger, err = zap.NewProduction()
	}
	if err != nil {
		panic(err)
	}
	Sugar = Logger.Sugar()
}

// Sync flushes any buffered log entries
func Sync() error {
	if Logger != nil {
		return Logger.Sync()
	}
	return nil
}

// Named returns a named logger
func Named(name string) *zap.Logger {
	if Logger == nil {
		InitDefault()
	}
	return Logger.Named(name)
}

// With creates a child logger with additional fields
func With(fields ...zap.Field) *zap.Logger {
	if Logger == nil {
		InitDefault()
	}
	return Logger.With(fields...)
}

// Debug logs at debug level
func Debug(msg string, fields ...zap.Field) {
	if Logger == nil {
		InitDefault()
	}
	Logger.Debug(msg, fields...)
}

// Info logs at info level
func Info(msg string, fields ...zap.Field) {
	if Logger == nil {
		InitDefault()
	}
	Logger.Info(msg, fields...)
}

// Warn logs at warn level
func Warn(msg string, fields ...zap.Field) {
	if Logger == nil {
		InitDefault()
	}
	Logger.Warn(msg, fields...)
}

// Error logs at error level
func Error(msg string, fields ...zap.Field) {
	if Logger == nil {
		InitDefault()
	}
	Logger.Error(msg, fields...)
}

// Fatal logs at fatal level and exits
func Fatal(msg string, fields ...zap.Field) {
	if Logger == nil {
		InitDefault()
	}
	Logger.Fatal(msg, fields...)
}

// Fields helper for common fields

// String creates a string field for structured logging.
func String(key, val string) zap.Field          { return zap.String(key, val) }

// Int creates an int field for structured logging.
func Int(key string, val int) zap.Field         { return zap.Int(key, val) }

// Int64 creates an int64 field for structured logging.
func Int64(key string, val int64) zap.Field     { return zap.Int64(key, val) }

// Err creates an error field for structured logging.
func Err(err error) zap.Field                   { return zap.Error(err) }

// Any creates a field for any value type for structured logging.
func Any(key string, val interface{}) zap.Field { return zap.Any(key, val) }
