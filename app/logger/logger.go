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
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	slogzerolog "github.com/samber/slog-zerolog/v2"
)

// SetDefaultsForLogger configures a global zerolog logger with sensible defaults.
// It uses a console writer with RFC3339 timestamps for human-readable output.
// If a log level is provided, it sets that level. Otherwise, it reads from the LOG_LEVEL
// environment variable (defaults to "info" if not set or invalid).
func SetDefaultsForLogger(_ context.Context, level ...zerolog.Level) {
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Determine effective level
	var effectiveLevel zerolog.Level

	if len(level) > 0 {
		effectiveLevel = level[0]
	} else {
		logLevel := os.Getenv("LOG_LEVEL")
		if logLevel == "" {
			logLevel = "info"
		}

		var err error

		effectiveLevel, err = zerolog.ParseLevel(logLevel)
		if err != nil {
			effectiveLevel = zerolog.InfoLevel

			log.Warn().Str("LOG_LEVEL", logLevel).Msg("Invalid log level, defaulting to info")
		}
	}

	// Apply to zerolog
	zerolog.SetGlobalLevel(effectiveLevel)

	// Apply to slog (via zerolog handler)
	slog.SetDefault(slog.New(slogzerolog.Option{
		Level:  zerologToSlog(effectiveLevel),
		Logger: &log.Logger,
	}.NewZerologHandler()))
}

// zerologToSlog maps zerolog levels to slog levels.
func zerologToSlog(level zerolog.Level) slog.Level {
	switch level {
	case zerolog.TraceLevel:
		return slog.LevelDebug - 4 //nolint:mnd // slog has no trace, use lower than debug
	case zerolog.DebugLevel:
		return slog.LevelDebug
	case zerolog.WarnLevel:
		return slog.LevelWarn
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		return slog.LevelError
	case zerolog.InfoLevel, zerolog.NoLevel, zerolog.Disabled:
		return slog.LevelInfo
	}

	return slog.LevelInfo
}
