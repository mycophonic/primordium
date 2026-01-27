package format

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSON formats output as indented JSON array.
type JSON struct{}

// PrintAll writes all data entries as a JSON array to the writer.
func (*JSON) PrintAll(data []*Data, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}

	return nil
}
