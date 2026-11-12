package main

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestValidFormOrigin(t *testing.T) {
    cases := []struct {
        name, origin, fetchSite, contentType string
        want bool
    }{
        {"same origin with browser Origin", publicOrigin, "same-origin", "application/x-www-form-urlencoded", true},
        {"same origin without browser Origin", "", "same-origin", "application/x-www-form-urlencoded", true},
        {"legacy browser with Origin", publicOrigin, "", "application/x-www-form-urlencoded", true},
        {"content type with charset", "", "same-origin", "application/x-www-form-urlencoded; charset=UTF-8", true},
        {"cross site metadata", publicOrigin, "cross-site", "application/x-www-form-urlencoded", false},
        {"sibling site metadata", publicOrigin, "same-site", "application/x-www-form-urlencoded", false},
        {"untrusted origin", "https://attacker.example", "same-origin", "application/x-www-form-urlencoded", false},
        {"null origin", "null", "same-origin", "application/x-www-form-urlencoded", false},
        {"unattributed request", "", "", "application/x-www-form-urlencoded", false},
        {"unsupported content type", publicOrigin, "same-origin", "text/plain", false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            r := httptest.NewRequest(http.MethodPost, publicOrigin+"/", nil)
            r.Header.Set("Content-Type", tc.contentType)
            if tc.origin != "" { r.Header.Set("Origin", tc.origin) }
            if tc.fetchSite != "" { r.Header.Set("Sec-Fetch-Site", tc.fetchSite) }
            if got := validFormOrigin(r); got != tc.want {
                t.Errorf("validFormOrigin() = %t, want %t", got, tc.want)
            }
        })
    }
}
