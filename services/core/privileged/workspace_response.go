package privileged

import (
	"bytes"
	"encoding/json"
	"errors"
)

// verifyWorkspaceToolResult checks the MCP tool envelope before an approved
// administrative request is recorded as accepted by the controller.
func verifyWorkspaceToolResult(data []byte) error {
	const denied = "Workspace controller operation was not accepted"
	var output struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Error   json.RawMessage `json:"error"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &output); err != nil || output.JSONRPC != "2.0" || output.ID != 1 ||
		len(output.Error) > 0 || len(output.Result) == 0 || bytes.Equal(bytes.TrimSpace(output.Result), []byte("null")) {
		return errors.New(denied)
	}
	var result struct {
		Content json.RawMessage `json:"content"`
		IsError json.RawMessage `json:"isError"`
	}
	if err := json.Unmarshal(output.Result, &result); err != nil || len(result.Content) == 0 {
		return errors.New(denied)
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(result.Content, &blocks); err != nil || blocks == nil {
		return errors.New(denied)
	}
	for _, block := range blocks {
		var content map[string]json.RawMessage
		if err := json.Unmarshal(block, &content); err != nil || content == nil {
			return errors.New(denied)
		}
	}
	// MCP 2025-06-18 defines isError as optional; absence means false.
	// Explicit nulls and non-boolean values are invalid, not successes.
	switch string(bytes.TrimSpace(result.IsError)) {
	case "", "false":
		return nil
	default:
		return errors.New(denied)
	}
}
