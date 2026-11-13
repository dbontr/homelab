package security

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestExecutorGrantIsOperationBoundAndSingleUse(t *testing.T) {
    state := filepath.Join(t.TempDir(), "replay.json")
    key := []byte(strings.Repeat("k", 32))
    authority, err := NewAuthority(key, state)
    if err != nil { t.Fatal(err) }
    args := json.RawMessage(`{"device":"jupiter","count":1}`)
    token, err := authority.Issue("cluster.inspect", "jupiter", args, time.Minute)
    if err != nil { t.Fatal(err) }
    cases := []struct {
        operation string
        target string
        args json.RawMessage
    }{
        {"cluster.modify", "jupiter", args},
        {"cluster.inspect", "venus", args},
        {"cluster.inspect", "jupiter", json.RawMessage(`{"device":"jupiter","count":2}`)},
    }
    for _, tc := range cases {
        if err := authority.Consume(token, tc.operation, tc.target, tc.args); err == nil {
            t.Fatalf("accepted altered scope: %s %s %s", tc.operation, tc.target, tc.args)
        }
    }
    if err := authority.Consume(token, "cluster.inspect", "jupiter", json.RawMessage(`{"count":1,"device":"jupiter"}`)); err != nil {
        t.Fatalf("equivalent canonical arguments rejected: %v", err)
    }
    if err := authority.Consume(token, "cluster.inspect", "jupiter", args); err == nil {
        t.Fatal("replayed approval was accepted")
    }
    restarted, err := NewAuthority(key, state)
    if err != nil { t.Fatal(err) }
    if err := restarted.Consume(token, "cluster.inspect", "jupiter", args); err == nil {
        t.Fatal("replayed approval was accepted after restart")
    }
}

func TestExecutorRejectsMalformedReplayLedgers(t *testing.T) {
    key := []byte(strings.Repeat("k", 32))
    for name, payload := range map[string]string{
        "empty": "",
        "invalid-json": "{",
        "null": "null",
        "bad-id": `{"not-a-nonce":123}`,
        "bad-expiry": `{"000000000000000000000000000000000000000000000000":-1}`,
        "oversized": strings.Repeat(" ", (1<<20)+1),
    } {
        t.Run(name, func(t *testing.T) {
            state := filepath.Join(t.TempDir(), "replay.json")
            if err := os.WriteFile(state, []byte(payload), 0600); err != nil { t.Fatal(err) }
            if _, err := NewAuthority(key, state); err == nil {
                t.Fatal("accepted invalid replay ledger")
            }
        })
    }
}

func TestExecutorRejectsPublicReplayState(t *testing.T) {
    state := filepath.Join(t.TempDir(), "replay.json")
    if err := os.WriteFile(state, []byte("{}"), 0644); err != nil { t.Fatal(err) }
    // The process umask must not make this deliberately unsafe fixture private.
    if err := os.Chmod(state, 0644); err != nil { t.Fatal(err) }
    if _, err := NewAuthority([]byte(strings.Repeat("k", 32)), state); err == nil {
        t.Fatal("accepted publicly readable replay ledger")
    }
}
