// Package logger provides a centralized logging facility using zap.
package logger

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	globalLogger *zap.Logger
	sugar        *zap.SugaredLogger
	once         sync.Once
	level        = zapcore.InfoLevel
)

// Config holds logger configuration.
type Config struct {
	// Debug enables debug level logging and console output.
	Debug bool
	// Output specifies the output file path. Empty means stdout.
	Output string
	// Level specifies the minimum log level.
	Level string
}

// Initialize initializes the global logger with the given configuration.
// This should be called once at application startup.
func Initialize(cfg Config) error {
	var err error
	once.Do(func() {
		err = initLogger(cfg)
	})
	return err
}

// initLogger creates and sets the global logger.
func initLogger(cfg Config) error {
	// Parse log level
	if cfg.Level != "" {
		if l, err := zapcore.ParseLevel(cfg.Level); err == nil {
			level = l
		}
	}
	if cfg.Debug {
		level = zapcore.DebugLevel
	}

	// Create encoder config
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Use console encoder for debug mode, JSON for production
	var encoder zapcore.Encoder
	if cfg.Debug {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// Create write syncer
	var writeSyncer zapcore.WriteSyncer
	if cfg.Output != "" {
		file, err := os.OpenFile(cfg.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		writeSyncer = zapcore.AddSync(file)
	} else {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	// Create core
	core := zapcore.NewCore(encoder, writeSyncer, level)

	// Create logger
	options := []zap.Option{
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	}
	if cfg.Debug {
		options = append(options, zap.Development())
	}

	globalLogger = zap.New(core, options...)
	sugar = globalLogger.Sugar()

	return nil
}

// L returns the global logger.
func L() *zap.Logger {
	if globalLogger == nil {
		// Initialize with defaults if not already initialized
		_ = Initialize(Config{})
	}
	return globalLogger
}

// S returns the global sugared logger.
func S() *zap.SugaredLogger {
	if sugar == nil {
		_ = Initialize(Config{})
	}
	return sugar
}

// Sync flushes any buffered log entries.
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}

// Debug logs a message at debug level.
func Debug(msg string, fields ...zap.Field) {
	L().Debug(msg, fields...)
}

// Info logs a message at info level.
func Info(msg string, fields ...zap.Field) {
	L().Info(msg, fields...)
}

// Warn logs a message at warn level.
func Warn(msg string, fields ...zap.Field) {
	L().Warn(msg, fields...)
}

// Error logs a message at error level.
func Error(msg string, fields ...zap.Field) {
	L().Error(msg, fields...)
}

// Fatal logs a message at fatal level and exits.
func Fatal(msg string, fields ...zap.Field) {
	L().Fatal(msg, fields...)
}

// With creates a child logger with additional fields.
func With(fields ...zap.Field) *zap.Logger {
	return L().With(fields...)
}

// Named adds a sub-logger name.
func Named(name string) *zap.Logger {
	return L().Named(name)
}

// SetLevel changes the global log level.
func SetLevel(l zapcore.Level) {
	level = l
	if globalLogger != nil {
		// Note: This doesn't dynamically change the level for existing logger
		// For dynamic level changes, we would need to use zapcore.AtomicLevel
	}
}