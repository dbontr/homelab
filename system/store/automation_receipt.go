package store

// taskAutomationReceipt is persisted with its task to make workflow retries
// safe after process restarts or ambiguous acknowledgments.
type taskAutomationReceipt struct {
 Fingerprint string `json:"fingerprint"`
 TaskID string `json:"task_id"`
}
