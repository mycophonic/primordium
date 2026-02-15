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

package format

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	headingChar     = "#"
	maxHeadingLevel = 6
	mdRuleSeparator = "---"
)

// Markdown formats output as structured markdown with tables.
type Markdown struct{}

// PrintAll writes all data entries with horizontal rule separators.
func (m *Markdown) PrintAll(data []*Data, writer io.Writer) error {
	for i, entry := range data {
		if i > 0 {
			if _, err := fmt.Fprintf(writer, "\n%s\n\n", mdRuleSeparator); err != nil {
				return fmt.Errorf("writing separator: %w", err)
			}
		}

		if err := m.printOne(entry, writer); err != nil {
			return err
		}
	}

	return nil
}

func (m *Markdown) printOne(data *Data, writer io.Writer) error {
	if _, err := fmt.Fprintf(writer, "## %s\n\n", data.Object); err != nil {
		return fmt.Errorf("writing title: %w", err)
	}

	if len(data.Meta) > 0 {
		return m.printMap(writer, data.Meta, 3)
	}

	return nil
}

func (m *Markdown) printMap(writer io.Writer, meta map[string]any, headingLevel int) error {
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		value := meta[key]
		if err := m.printValue(writer, key, value, headingLevel); err != nil {
			return err
		}
	}

	return nil
}

func (m *Markdown) printValue(writer io.Writer, key string, value any, headingLevel int) error {
	switch typedValue := value.(type) {
	case map[string]any:
		return m.printMapSection(writer, key, typedValue, headingLevel)
	case []any:
		return m.printSliceSection(writer, key, typedValue, headingLevel)
	default:
		return nil
	}
}

func (m *Markdown) printMapSection(
	writer io.Writer,
	key string,
	data map[string]any,
	headingLevel int,
) error {
	heading := strings.Repeat(headingChar, min(headingLevel, maxHeadingLevel))

	if _, err := fmt.Fprintf(writer, "%s %s\n\n", heading, key); err != nil {
		return fmt.Errorf("writing heading %s: %w", key, err)
	}

	scalarFields, nestedFields := separateFields(data)

	if len(scalarFields) > 0 {
		if err := m.printTable(writer, scalarFields); err != nil {
			return err
		}
	}

	for _, nestedKey := range sortedKeys(nestedFields) {
		if err := m.printValue(writer, nestedKey, nestedFields[nestedKey], headingLevel+1); err != nil {
			return err
		}
	}

	return nil
}

func (m *Markdown) printSliceSection(
	writer io.Writer,
	key string,
	slice []any,
	headingLevel int,
) error {
	heading := strings.Repeat(headingChar, min(headingLevel, maxHeadingLevel))

	if _, err := fmt.Fprintf(writer, "%s %s\n\n", heading, key); err != nil {
		return fmt.Errorf("writing heading %s: %w", key, err)
	}

	for index, item := range slice {
		switch typedItem := item.(type) {
		case map[string]any:
			itemHeading := strings.Repeat(headingChar, min(headingLevel+1, maxHeadingLevel))

			if _, err := fmt.Fprintf(writer, "%s Stream %d\n\n", itemHeading, index+1); err != nil {
				return fmt.Errorf("writing stream %d heading: %w", index, err)
			}

			scalarFields, nestedFields := separateFields(typedItem)

			if len(scalarFields) > 0 {
				if err := m.printTable(writer, scalarFields); err != nil {
					return err
				}
			}

			for _, nestedKey := range sortedKeys(nestedFields) {
				if err := m.printValue(writer, nestedKey, nestedFields[nestedKey], headingLevel+2); err != nil {
					return err
				}
			}
		default:
			if _, err := fmt.Fprintf(writer, "- %v\n", typedItem); err != nil {
				return fmt.Errorf("writing item %d: %w", index, err)
			}
		}
	}

	return nil
}

func (*Markdown) printTable(writer io.Writer, fields map[string]any) error {
	// Check for spectrogram and handle specially
	spectrogramPath, hasSpectrogram := fields["spectrogram_path"].(string)
	if hasSpectrogram {
		// Remove spectrogram fields from table, will render as image below
		delete(fields, "spectrogram_path")
		delete(fields, "spectrogram_rel_path")
	}

	if len(fields) > 0 {
		if _, err := fmt.Fprintln(writer, "| Field | Value |"); err != nil {
			return fmt.Errorf("writing table header: %w", err)
		}

		if _, err := fmt.Fprintln(writer, "|-------|-------|"); err != nil {
			return fmt.Errorf("writing table separator: %w", err)
		}

		for _, key := range sortedKeys(fields) {
			if _, err := fmt.Fprintf(writer, "| %s | %v |\n", key, fields[key]); err != nil {
				return fmt.Errorf("writing table row %s: %w", key, err)
			}
		}

		if _, err := fmt.Fprintln(writer); err != nil {
			return fmt.Errorf("writing table trailing newline: %w", err)
		}
	}

	// Render spectrogram as embedded image
	if hasSpectrogram && spectrogramPath != "" {
		if _, err := fmt.Fprintf(writer, "**Spectrogram:**\n\n![Spectrogram](%s)\n\n", spectrogramPath); err != nil {
			return fmt.Errorf("writing spectrogram: %w", err)
		}
	}

	return nil
}

func separateFields(data map[string]any) (scalars, nested map[string]any) {
	scalars = make(map[string]any)
	nested = make(map[string]any)

	for key, value := range data {
		switch value.(type) {
		case map[string]any, []any:
			nested[key] = value
		default:
			scalars[key] = value
		}
	}

	return scalars, nested
}

func sortedKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
