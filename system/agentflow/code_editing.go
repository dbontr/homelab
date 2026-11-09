package agentflow

import (
    "errors"
    "go/format"
)

// formatGoSource applies the Go parser and formatter to complete source text.
// The caller must enforce file allowlists and expected source digests.
func formatGoSource(source string) (string, error) {
    if len(source) == 0 || len(source) > 24000 {
        return "", errors.New("source is outside formatting limits")
    }
    formatted, err := format.Source([]byte(source))
    if err != nil {
        return "", errors.New("Go source has syntax errors")
    }
    if len(formatted) == 0 || len(formatted) > 24000 {
        return "", errors.New("formatted source exceeds file limit")
    }
    return string(formatted), nil
}
