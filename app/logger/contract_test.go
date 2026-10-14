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

package logger_test

// The contract, from logger's docs:
//   - the level is the argument, else LOG_LEVEL (trace and debug, info,
//     warn and warning, error; case and spaces ignored), else info, and an
//     unknown LOG_LEVEL is info with a warning;
//   - the result is true when the effective level is debug or lower;
//   - stderr off a terminal gets JSON records.
//
// The default logger and LOG_LEVEL are process-wide, so the tests run in
// order; the one reading stderr runs this binary as a child, told by
// PRIMORDIUM_LOGGER_CHILD.

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/app/logger"
)

const childEnv = "PRIMORDIUM_LOGGER_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "log":
		logger.SetDefaultsForLogger(context.Background())
		slog.Info("hello", "answer", 42)
	default:
		os.Exit(99)
	}
}

func restoreDefault(t *testing.T) {
	t.Helper()

	previous := slog.Default()

	t.Cleanup(func() { slog.SetDefault(previous) })
}

// at reports whether the default logger is enabled at level and not below it.
func at(t *testing.T, level slog.Level) bool {
	t.Helper()

	return slog.Default().Enabled(t.Context(), level) && !slog.Default().Enabled(t.Context(), level-1)
}

//nolint:paralleltest // process-wide default logger
func TestLevelArgument(t *testing.T) {
	restoreDefault(t)

	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		debug := logger.SetDefaultsForLogger(t.Context(), level)

		if debug != (level <= slog.LevelDebug) {
			t.Errorf("level %v: debug %v", level, debug)
		}

		if !at(t, level) {
			t.Errorf("level %v: the default logger is not at it", level)
		}
	}
}

//nolint:paralleltest // process-wide default logger and LOG_LEVEL
func TestLevelEnvironment(t *testing.T) {
	restoreDefault(t)

	for _, tc := range []struct {
		value string
		want  slog.Level
	}{
		{"", slog.LevelInfo},
		{"trace", slog.LevelDebug},
		{" DEBUG ", slog.LevelDebug},
		{"Info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"Warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"verbose", slog.LevelInfo},
		{"3", slog.LevelInfo},
		{"debugging", slog.LevelInfo},
	} {
		t.Setenv("LOG_LEVEL", tc.value)

		debug := logger.SetDefaultsForLogger(t.Context())

		if debug != (tc.want <= slog.LevelDebug) {
			t.Errorf("LOG_LEVEL=%q: debug %v", tc.value, debug)
		}

		if !at(t, tc.want) {
			t.Errorf("LOG_LEVEL=%q: the default logger is not at %v", tc.value, tc.want)
		}
	}
}

// TestJSONOffATerminal: a child with an unknown LOG_LEVEL writes the warning
// and then its record, both JSON, to a stderr that is a pipe.
func TestJSONOffATerminal(t *testing.T) {
	t.Parallel()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)

	cmd.Env = append(os.Environ(), childEnv+"=log", "LOG_LEVEL=nonsense")

	var stderr strings.Builder

	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines, want the warning and the record:\n%s", len(lines), stderr.String())
	}

	if !strings.Contains(lines[0], `"level":"WARN"`) || !strings.Contains(lines[0], `"log_level":"nonsense"`) {
		t.Errorf("first line is not the LOG_LEVEL warning: %s", lines[0])
	}

	if !strings.HasPrefix(lines[1], "{") || !strings.Contains(lines[1], `"msg":"hello"`) ||
		!strings.Contains(lines[1], `"answer":42`) {
		t.Errorf("record is not JSON with the attributes: %s", lines[1])
	}
}
