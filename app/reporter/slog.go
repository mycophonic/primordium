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

package reporter

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/getsentry/sentry-go"
)

// sentryHandler decorates an inner slog.Handler, mirroring every record to Sentry
// on top of writing it through inner (the console handler). Records at ERROR level
// and above are captured as Sentry events (issues); everything below is recorded as
// a breadcrumb — buffered context Sentry attaches to the next event, so the log
// trail that led to an error travels with it. inner always runs, so console output
// is unchanged; nothing is swallowed.
//
// This is the errors→issues + logs→breadcrumbs pattern: it keeps issue volume low
// (only real errors become alertable events) while preserving the surrounding log
// context for free. It does NOT use Sentry's separate Logs product.
type sentryHandler struct {
	inner  slog.Handler
	attrs  []slog.Attr
	groups []string
}

// newSentryHandler wraps inner so slog output is mirrored to Sentry. Initialize
// installs it as the process default after sentry.Init (the client must exist first).
func newSentryHandler(inner slog.Handler) slog.Handler {
	return &sentryHandler{inner: inner}
}

// Enabled mirrors the inner handler, so Sentry sees exactly the records the console
// shows — one LOG_LEVEL governs both; Handle routes by level.
func (h *sentryHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *sentryHandler) Handle(ctx context.Context, rec slog.Record) error {
	data, err := h.collect(rec)

	if rec.Level >= slog.LevelError {
		captureErrorEvent(rec, data, err)
	} else {
		sentry.AddBreadcrumb(&sentry.Breadcrumb{
			Type:      "default",
			Category:  "log",
			Message:   rec.Message,
			Level:     sentryLevel(rec.Level),
			Data:      data,
			Timestamp: rec.Time,
		})
	}

	if err := h.inner.Handle(ctx, rec); err != nil {
		return fmt.Errorf("console handler: %w", err)
	}

	return nil
}

func (h *sentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sentryHandler{
		inner:  h.inner.WithAttrs(attrs),
		attrs:  append(slices.Clip(h.attrs), attrs...),
		groups: h.groups,
	}
}

func (h *sentryHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	return &sentryHandler{
		inner:  h.inner.WithGroup(name),
		attrs:  h.attrs,
		groups: append(slices.Clip(h.groups), name),
	}
}

// collect flattens the handler's accumulated attrs plus the record's own into a
// Sentry data map (open groups become dotted key prefixes) and returns the first
// error-typed value it finds, so an ERROR record logged with an `err` attribute is
// captured as a real exception (grouped, stacktraced) rather than a bare message.
func (h *sentryHandler) collect(rec slog.Record) (map[string]any, error) {
	prefix := ""
	if len(h.groups) > 0 {
		prefix = strings.Join(h.groups, ".") + "."
	}

	data := make(map[string]any, len(h.attrs)+rec.NumAttrs())

	var errVal error

	add := func(a slog.Attr) {
		value := a.Value.Resolve().Any()
		data[prefix+a.Key] = value

		if errVal == nil {
			if e, ok := value.(error); ok {
				errVal = e
			}
		}
	}

	for _, a := range h.attrs {
		add(a)
	}

	rec.Attrs(func(a slog.Attr) bool {
		add(a)

		return true
	})

	return data, errVal
}

// captureErrorEvent sends an ERROR-level record to Sentry as an event, scoped with the
// record's attributes under a "log" context. A captured error value becomes a
// proper exception; otherwise the message is captured.
func captureErrorEvent(rec slog.Record, data map[string]any, err error) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentryLevel(rec.Level))

		if len(data) > 0 {
			scope.SetContext("log", data)
		}

		if err != nil {
			sentry.CaptureException(err)
		} else {
			sentry.CaptureMessage(rec.Message)
		}
	})
}

// sentryLevel maps a slog level onto Sentry's coarser scale.
func sentryLevel(level slog.Level) sentry.Level {
	switch {
	case level >= slog.LevelError:
		return sentry.LevelError
	case level >= slog.LevelWarn:
		return sentry.LevelWarning
	case level >= slog.LevelInfo:
		return sentry.LevelInfo
	default:
		return sentry.LevelDebug
	}
}
