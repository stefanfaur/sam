package tools

import (
	"encoding/json"
	"strconv"
)

// flexInt unmarshals either a JSON number or a numeric string. Models
// sometimes emit tool-call args with stringified numbers; this type lets
// tool inputs survive that without a custom UnmarshalJSON per struct.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	if len(b) >= 2 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*f = 0
			return nil
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*f = flexInt(n)
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}
