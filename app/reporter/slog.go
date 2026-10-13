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

// sentryHandler mirrors every record to Sentry and passes it to inner, the
// console handler: ERROR and above as events, the rest as breadcrumbs that
// travel with the next event. inner always runs; nothing is swallowed.
type sentryHandler struct {
	inner  slog.Handler
	attrs  []slog.Attr
	groups []string
}

func newSentryHandler(inner slog.Handler) slog.Handler {
	return &sentryHandler{inner: inner}
}

// Enabled is inner's: one level governs the console and Sentry.
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

// collect flattens the handler's attrs and the record's into one map, open
// groups as dotted prefixes, and returns the first error-typed value: that
// error is what Sentry groups and stack-traces, not the message.
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

// captureErrorEvent sends the record as an event with its attributes under
// the "log" context: the error value when there is one, else the message.
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
