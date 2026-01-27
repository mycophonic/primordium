package format

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	indentUnit    = "  "
	ruleSeparator = "────────────────────────────────────────────────────────────────────────────────"
)

func wrapKeyError(key string, err error) error {
	return fmt.Errorf("writing key %q: %w", key, err)
}

// Console formats output as human-readable key-value pairs.
type Console struct{}

// PrintAll writes all data entries with horizontal rule separators.
func (c *Console) PrintAll(data []*Data, writer io.Writer) error {
	for i, entry := range data {
		if i > 0 {
			if _, err := fmt.Fprintf(writer, "\n%s\n\n", ruleSeparator); err != nil {
				return fmt.Errorf("writing separator: %w", err)
			}
		}

		if err := c.printOne(entry, writer); err != nil {
			return err
		}
	}

	return nil
}

func (c *Console) printOne(data *Data, writer io.Writer) error {
	if _, err := fmt.Fprintf(writer, "Path: %s\n", data.Object); err != nil {
		return fmt.Errorf("writing path: %w", err)
	}

	if len(data.Meta) > 0 {
		if _, err := fmt.Fprintln(writer); err != nil {
			return fmt.Errorf("writing newline: %w", err)
		}

		return c.printMap(writer, data.Meta, 0)
	}

	return nil
}

func (c *Console) printMap(writer io.Writer, meta map[string]any, indent int) error {
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	prefix := strings.Repeat(indentUnit, indent)

	for _, key := range keys {
		value := meta[key]
		if err := c.printValue(writer, key, value, prefix, indent); err != nil {
			return err
		}
	}

	return nil
}

func (c *Console) printValue(
	writer io.Writer,
	key string,
	value any,
	prefix string,
	indent int,
) error {
	switch typedValue := value.(type) {
	case map[string]any:
		if _, err := fmt.Fprintf(writer, "%s%s:\n", prefix, key); err != nil {
			return wrapKeyError(key, err)
		}

		return c.printMap(writer, typedValue, indent+1)
	case []any:
		if _, err := fmt.Fprintf(writer, "%s%s:\n", prefix, key); err != nil {
			return wrapKeyError(key, err)
		}

		return c.printSlice(writer, typedValue, indent+1)
	default:
		if _, err := fmt.Fprintf(writer, "%s%s: %v\n", prefix, key, typedValue); err != nil {
			return wrapKeyError(key, err)
		}

		return nil
	}
}

func (c *Console) printSlice(writer io.Writer, slice []any, indent int) error {
	prefix := strings.Repeat(indentUnit, indent)

	for index, item := range slice {
		switch typedItem := item.(type) {
		case map[string]any:
			if _, err := fmt.Fprintf(writer, "%s[%d]:\n", prefix, index); err != nil {
				return fmt.Errorf("writing index %d: %w", index, err)
			}

			if err := c.printMap(writer, typedItem, indent+1); err != nil {
				return err
			}
		default:
			if _, err := fmt.Fprintf(writer, "%s[%d]: %v\n", prefix, index, typedItem); err != nil {
				return fmt.Errorf("writing index %d: %w", index, err)
			}
		}
	}

	return nil
}
