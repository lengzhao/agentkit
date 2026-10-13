package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// validateInputs checks tool-call args against the workflow's declared inputs
// schema. A workflow without inputs accepts any args.
func validateInputs(wf *Workflow, args map[string]any) error {
	if len(wf.Inputs) == 0 {
		return nil
	}
	if err := validateAgainstSchema(wf.Inputs, args); err != nil {
		return fmt.Errorf("workflow %q: args do not match inputs schema: %w", wf.Name, err)
	}
	return nil
}

func validateAgainstSchema(schema map[string]any, value any) error {
	raw, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("encode schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", doc); err != nil {
		return fmt.Errorf("load schema: %w", err)
	}
	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return err
	}
	return nil
}
