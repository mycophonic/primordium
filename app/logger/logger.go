/*
   Copyright Mycophonic.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"

	"github.com/mycophonic/primordium/term"
)

// SetDefaultsForLogger installs the process-wide slog handler. When stderr is a
// terminal it uses lmittmann/tint for colored, human-readable console output with
// RFC3339 timestamps; when stderr is redirected (file, pipe, CI) it falls back to
// slog's JSON handler so logs stay machine-parseable.
//
// The level comes from the optional argument, else the LOG_LEVEL environment
// variable (debug/info/warn/error — "trace" maps to debug, which slog has no
// distinct level for), defaulting to info. It returns true when the effective
// level is debug or lower, for callers that gate debug-only behavior (e.g. the
// Sentry reporter).
func SetDefaultsForLogger(_ context.Context, level ...slog.Level) bool {
	var (
		effective slog.Level
		badEnv    string
	)

	if len(level) > 0 {
		effective = level[0]
	} else if lvl, ok := parseLevel(os.Getenv("LOG_LEVEL")); ok {
		effective = lvl
	} else {
		effective = slog.LevelInfo
		badEnv = os.Getenv("LOG_LEVEL")
	}

	var handler slog.Handler
	if term.IsTerminal(os.Stderr.Fd()) {
		handler = tint.NewTextHandler(os.Stderr, &tint.Options{Level: effective, TimeFormat: time.RFC3339})
	} else {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: effective})
	}

	slog.SetDefault(slog.New(handler))

	if badEnv != "" {
		slog.Warn("invalid LOG_LEVEL, defaulting to info", "LOG_LEVEL", badEnv)
	}

	return effective <= slog.LevelDebug
}

// parseLevel maps a LOG_LEVEL string to a slog.Level. slog has no trace level, so
// "trace" collapses to debug. ok is false only for a non-empty, unrecognized value
// (so the caller can warn); an empty value is a valid "use the default" and returns info.
func parseLevel(value string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, true
	case "trace", "debug":
		return slog.LevelDebug, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}
