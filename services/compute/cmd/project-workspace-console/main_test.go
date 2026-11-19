package main

import "testing"

func TestValidConsoleSession(t *testing.T) {
	base := map[string]string{
		"User": "0", "Seat": "seat0", "Type": "tty", "Remote": "no",
		"Service": "login", "TTY": "tty1",
	}
	copyProperties := func() map[string]string {
		c := make(map[string]string, len(base))
		for key, value := range base {
			c[key] = value
		}
		return c
	}
	tests := []struct {
		name       string
		uid        int
		tty        string
		session    string
		properties map[string]string
		allowed    bool
	}{
		{"local root", 0, "/dev/tty1", "8", copyProperties(), true},
		{"nonroot", 1000, "/dev/tty1", "8", copyProperties(), false},
		{"remote pseudo terminal", 0, "/dev/pts/1", "8", copyProperties(), false},
		{"invalid session id", 0, "/dev/tty1", "evil", copyProperties(), false},
	}
	remote := copyProperties()
	remote["Remote"] = "yes"
	tests = append(tests, struct {
		name       string
		uid        int
		tty        string
		session    string
		properties map[string]string
		allowed    bool
	}{"remote login", 0, "/dev/tty1", "8", remote, false})
	wrongSeat := copyProperties()
	wrongSeat["Seat"] = "seat1"
	tests = append(tests, struct {
		name       string
		uid        int
		tty        string
		session    string
		properties map[string]string
		allowed    bool
	}{"different seat", 0, "/dev/tty1", "8", wrongSeat, false})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validConsoleSession(test.uid, test.tty, test.session, test.properties)
			if got != test.allowed {
				t.Fatalf("validConsoleSession() = %t, want %t", got, test.allowed)
			}
		})
	}
}
