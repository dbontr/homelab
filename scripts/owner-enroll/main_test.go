package main

import (
    "net/http"
    "net/http/httptest"
    "net/url"
    "strings"
    "testing"
)

func TestValidateEnrollmentForm(t *testing.T) {
    const token = "unpredictable-csrf-token"
    cases := []struct {
        name, origin, contentType, submittedToken string
        want bool
    }{
        {"browser with expected origin", publicOrigin, "application/x-www-form-urlencoded", token, true},
        {"browser with absent origin", "", "application/x-www-form-urlencoded", token, true},
        {"proxy altered origin with valid form token", "null", "application/x-www-form-urlencoded", token, true},
        {"cross-origin with valid form token", "https://other.example", "application/x-www-form-urlencoded", token, true},
        {"content type with charset", "", "application/x-www-form-urlencoded; charset=UTF-8", token, true},
        {"missing token", "", "application/x-www-form-urlencoded", "", false},
        {"wrong token", publicOrigin, "application/x-www-form-urlencoded", "incorrect", false},
        {"cross-origin without form token", "https://other.example", "application/x-www-form-urlencoded", "", false},
        {"unsupported body encoding", publicOrigin, "text/plain", token, false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            body := url.Values{"csrf": {tc.submittedToken}, "password": {"unused"}}
            r := httptest.NewRequest(http.MethodPost, publicOrigin+"/", strings.NewReader(body.Encode()))
            r.Header.Set("Content-Type", tc.contentType)
            if tc.origin != "" { r.Header.Set("Origin", tc.origin) }
            if got := validateEnrollmentForm(r, token); got != tc.want {
                t.Errorf("validateEnrollmentForm() = %t, want %t", got, tc.want)
            }
        })
    }
}
