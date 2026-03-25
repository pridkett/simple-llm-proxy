package logger

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
)

// ANSI color escape sequences used by the console formatter.
const (
	colorReset   = "\x1b[0m"
	colorBold    = "\x1b[1m"
	colorDim     = "\x1b[2m"
	colorRed     = "\x1b[31m"
	colorGreen   = "\x1b[32m"
	colorYellow  = "\x1b[33m"
	colorBlue    = "\x1b[34m"
	colorMagenta = "\x1b[35m"
	colorCyan    = "\x1b[36m"
	colorOrange  = "\x1b[38;5;208m"
)

// Compiled patterns for value-type colorization in formatFieldValue.
var (
	reUUID   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reNumber = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	reQuoted = regexp.MustCompile(`^".*"$`)
)

// Init configures zerolog's global logger from LogSettings.
// Must be called once, early in main(), before any other logging.
func Init(ls config.LogSettings) {
	// Parse log level; fall back to info on invalid input.
	level, err := zerolog.ParseLevel(ls.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	var writers []io.Writer

	// Console writer: colored text → stderr (suppressed when format=json).
	if ls.Format != "json" {
		cw := zerolog.ConsoleWriter{
			Out:              os.Stderr,
			TimeFormat:       time.RFC3339,
			FormatTimestamp:  formatTimestamp,
			FormatLevel:      formatLevel,
			FormatFieldName:  formatFieldName,
			FormatFieldValue: formatFieldValue,
			FormatMessage:    formatMessage,
		}
		writers = append(writers, cw)
	} else {
		// Raw JSON to stderr when format=json.
		writers = append(writers, os.Stderr)
	}

	// Optional rotating JSON file writer.
	if ls.FilePath != "" {
		rotator := &lumberjack.Logger{
			Filename:   ls.FilePath,
			MaxSize:    ls.MaxSizeMB,
			MaxBackups: ls.MaxBackups,
			MaxAge:     ls.MaxAgeDays,
			Compress:   ls.Compress,
		}
		writers = append(writers, rotator)
	}

	// Fan-out: ConsoleWriter gets colored text; file gets raw JSON.
	var w io.Writer
	if len(writers) == 1 {
		w = writers[0]
	} else {
		w = zerolog.MultiLevelWriter(writers...)
	}

	log.Logger = zerolog.New(w).
		With().
		Timestamp().
		Logger()
}

// Component returns a child logger with the "component" field pre-set.
// Useful for packages that want to tag all their output without repeating the field.
func Component(name string) zerolog.Logger {
	return log.Logger.With().Str("component", name).Logger()
}

// formatTimestamp renders timestamps in dim color to reduce visual noise.
func formatTimestamp(i any) string {
	return colorDim + fmt.Sprint(i) + colorReset
}

// formatLevel renders log level labels with a distinct color per severity.
func formatLevel(i any) string {
	l, ok := i.(string)
	if !ok {
		return fmt.Sprint(i)
	}
	switch strings.ToUpper(l) {
	case "TRACE":
		return colorDim + "TRC" + colorReset
	case "DEBUG":
		return colorGreen + "DBG" + colorReset
	case "INFO":
		return colorBlue + "INF" + colorReset
	case "WARN":
		return colorOrange + "WRN" + colorReset
	case "ERROR":
		return colorRed + "ERR" + colorReset
	case "FATAL":
		return colorRed + colorBold + "FTL" + colorReset
	default:
		return strings.ToUpper(l)
	}
}

// formatFieldName renders field keys in bold so they stand out from values.
func formatFieldName(i any) string {
	return colorBold + fmt.Sprint(i) + "=" + colorReset
}

// formatMessage renders the log message without extra styling.
func formatMessage(i any) string {
	return fmt.Sprint(i)
}

// formatFieldValue colorizes values by their inferred type:
//   - UUIDs → magenta
//   - Numbers and duration strings → yellow
//   - Quoted strings → cyan
//   - Everything else → unstyled
func formatFieldValue(i any) string {
	s := fmt.Sprint(i)
	if reUUID.MatchString(s) {
		return colorMagenta + s + colorReset
	}
	if reNumber.MatchString(s) {
		return colorYellow + s + colorReset
	}
	if reQuoted.MatchString(s) {
		return colorCyan + s + colorReset
	}
	if looksLikeDuration(s) {
		return colorYellow + s + colorReset
	}
	return s
}

// looksLikeDuration returns true for Go duration strings like "123.4ms", "1.2s", "500µs".
func looksLikeDuration(s string) bool {
	if len(s) < 2 {
		return false
	}
	for _, suffix := range []string{"ms", "µs", "ns"} {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	// bare seconds: digits optionally with decimal, ending in 's'
	if s[len(s)-1] == 's' {
		return reNumber.MatchString(s[:len(s)-1])
	}
	return false
}
