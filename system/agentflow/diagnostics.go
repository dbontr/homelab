package agentflow

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

type CheckDiagnostic struct {
	Command   string  `json:"command"`
	ExitCode  int     `json:"exit_code"`
	Category  string  `json:"category"`
	OutputSHA string  `json:"output_sha256"`
	Coverage  float64 `json:"coverage_percent,omitempty"`
}

var coveragePattern = regexp.MustCompile(`(?m)total:\s*\S+\s+([0-9]+(?:\.[0-9]+)?)%`)

func classifyCheck(command string, exitCode int, stdout, stderr string) CheckDiagnostic {
	output := stdout + "\n" + stderr
	sum := sha256.Sum256([]byte(output))
	report := CheckDiagnostic{Command: command, ExitCode: exitCode,
		Category: "passed", OutputSHA: hex.EncodeToString(sum[:])}
	if exitCode != 0 {
		normalized := strings.ToLower(output)
		switch {
		case strings.Contains(normalized, "panic:") || strings.Contains(normalized, "test failed") ||
			strings.Contains(normalized, "assertion") || strings.Contains(normalized, "--- fail:"):
			report.Category = "test_failure"
		case strings.Contains(normalized, "syntax error") || strings.Contains(normalized, "cannot use") ||
			strings.Contains(normalized, "undefined:") || strings.Contains(normalized, "error[e"):
			report.Category = "compilation"
		case strings.Contains(normalized, "not found") || strings.Contains(normalized, "no such file") ||
			strings.Contains(normalized, "missing") || strings.Contains(normalized, "unavailable"):
			report.Category = "missing_dependency"
		case strings.Contains(normalized, "timed out") || strings.Contains(normalized, "deadline exceeded"):
			report.Category = "timeout"
		default:
			report.Category = "unclassified_failure"
		}
	}
	if match := coveragePattern.FindStringSubmatch(output); len(match) == 2 {
		coverage, err := strconv.ParseFloat(match[1], 64)
		if err == nil && coverage >= 0 && coverage <= 100 {
			report.Coverage = coverage
		}
	}
	return report
}
