package tools

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

func SchemaOf[In any]() map[string]any {
	r := &jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
		ExpandedStruct:            true,
	}
	s := r.Reflect(new(In))
	b, _ := json.Marshal(s)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	// strip "$schema", "$id", "$defs"; enforce additionalProperties:false at top
	delete(out, "$schema")
	delete(out, "$id")
	delete(out, "$defs")
	out["additionalProperties"] = false
	return out
}