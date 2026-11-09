package store

import "time"

// taskAutomationReceipt is persisted with its task to make workflow retries
// safe after process restarts or ambiguous acknowledgments.
type taskAutomationReceipt struct {
 Fingerprint string `json:"fingerprint"`
 TaskID string `json:"task_id,omitempty"`
 GoalID string `json:"goal_id,omitempty"`
 Action string `json:"action,omitempty"`
 ConfirmedAt time.Time `json:"confirmed_at,omitempty"`
}
