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
    tests := []struct {
        name, origin, contentType, csrf string
        extraField, malformed string
        want bool
    }{
        {name:"normal form",origin:publicOrigin,contentType:"application/x-www-form-urlencoded",csrf:token,want:true},
        {name:"missing origin and content type",csrf:token,want:true},
        {name:"proxy changed origin and content type",origin:"null",contentType:"text/plain",csrf:token,want:true},
        {name:"hostile origin with known CSRF",origin:"https://other.example",csrf:token,want:true},
        {name:"untrusted origin without CSRF",origin:"https://other.example",want:false},
        {name:"missing CSRF",want:false},
        {name:"incorrect CSRF",csrf:"wrong",want:false},
        {name:"duplicate CSRF",csrf:token,extraField:"csrf=another",want:false},
        {name:"malformed encoding",csrf:token,malformed:"%Q0",want:false},
        {name:"duplicate password",csrf:token,extraField:"password=another",want:false},
    }
    for _,tc:=range tests {
        t.Run(tc.name,func(t *testing.T){
            values:=url.Values{"csrf":{tc.csrf},"password":{"unused"},"confirm":{"unused"},"totp":{"unused"}}
            body:=values.Encode()
            if tc.extraField!=""{body+="&"+tc.extraField}
            if tc.malformed!=""{body+="&"+tc.malformed}
            r:=httptest.NewRequest(http.MethodPost,publicOrigin+"/",strings.NewReader(body))
            if tc.origin!=""{r.Header.Set("Origin",tc.origin)}
            if tc.contentType!=""{r.Header.Set("Content-Type",tc.contentType)}
            if got:=validateEnrollmentForm(r,token);got!=tc.want{
                t.Errorf("validateEnrollmentForm() = %t, want %t",got,tc.want)
            }
        })
    }
}

func TestValidateEnrollmentFormBodyLimit(t *testing.T) {
    const token="unpredictable-csrf-token"
    values:=url.Values{"csrf":{token},"password":{strings.Repeat("x",5000)},"confirm":{"unused"},"totp":{"unused"}}
    r:=httptest.NewRequest(http.MethodPost,publicOrigin+"/",strings.NewReader(values.Encode()))
    w:=httptest.NewRecorder()
    r.Body=http.MaxBytesReader(w,r.Body,4096)
    if validateEnrollmentForm(r,token) {t.Fatal("oversized form accepted")}
}
