package calendarhttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const calendarSecret = "calendar-service-identity-with-sufficient-length"

func TestCalendarServiceCredentialAndPrivateSource(t *testing.T) {
	var feedRequests atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feedRequests.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
			t.Error("calendar provider received an unexpected method or credential")
		}
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:seminar\r\nDTSTART:20261109T120000Z\r\nSUMMARY:Seminar\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	}))
	defer provider.Close()
	handler, err := NewHandler(provider.URL, calendarSecret, provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	service := httptest.NewServer(handler)
	defer service.Close()
	for _,tc := range []struct{method,path,token,origin string; status int}{
		{http.MethodGet,"/v1/calendar","", "",http.StatusUnauthorized},
		{http.MethodGet,"/v1/calendar","invalid", "",http.StatusUnauthorized},
		{http.MethodGet,"/v1/calendar?url=https://attacker.example",calendarSecret,"",http.StatusNotFound},
		{http.MethodPost,"/v1/calendar",calendarSecret,"",http.StatusNotFound},
		{http.MethodGet,"/mcp",calendarSecret,"",http.StatusNotFound},
		{http.MethodGet,"/v1/calendar",calendarSecret,"https://attacker.example",http.StatusNotFound},
	}{
		req:=httptest.NewRequest(tc.method,tc.path,nil)
		if tc.token!=""{req.Header.Set("Authorization","Bearer "+tc.token)}
		if tc.origin!=""{req.Header.Set("Origin",tc.origin)}
		out:=httptest.NewRecorder()
		handler.ServeHTTP(out,req)
		if out.Code!=tc.status{t.Fatalf("%s %s: %d, expected %d",tc.method,tc.path,out.Code,tc.status)}
	}
	if feedRequests.Load()!=0{t.Fatal("unauthorized requests reached the private provider")}
	client,err:=NewClient(service.URL,calendarSecret)
	if err!=nil{t.Fatal(err)}
	feed,err:=client.Fetch(context.Background())
	if err!=nil{t.Fatal(err)}
	if len(feed.Events)!=1||feed.Events[0].Summary!="Seminar"{t.Fatalf("calendar event lost: %+v",feed)}
	if feedRequests.Load()!=1{t.Fatalf("calendar provider called %d times",feedRequests.Load())}
	if _,err:=NewClient(service.URL,"short");err==nil{t.Fatal("weak calendar credentials admitted")}
	if _,err:=NewClient("https://calendar.example",calendarSecret);err==nil{t.Fatal("external calendar service origin admitted")}
	if _,err:=NewClient(service.URL+"/arbitrary",calendarSecret);err==nil{t.Fatal("calendar service path override admitted")}
	if _,err:=NewClient("http://192.0.2.20:8420",calendarSecret);err==nil{t.Fatal("non-loopback calendar service admitted")}
}

func TestUnavailableCalendarFailsClosed(t *testing.T){
	service:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		w.WriteHeader(http.StatusBadGateway)
		_,_=io.WriteString(w,"upstream failure")
	}))
	client,err:=NewClient(service.URL,calendarSecret)
	if err!=nil{t.Fatal(err)}
	service.Close()
	if _,err:=client.Fetch(context.Background());err==nil{
		t.Fatal("unavailable calendar server was treated as valid availability")
	}
}

func TestLargeCalendarReplyIsRejected(t *testing.T) {
	service:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){
		w.Header().Set("Content-Type","application/json")
		_,_=io.WriteString(w,strings.Repeat(" ",maxFeedBytes+2))
	}))
	defer service.Close()
	client,err:=NewClient(service.URL,calendarSecret)
	if err!=nil{t.Fatal(err)}
	if _,err:=client.Fetch(context.Background());err==nil{
		t.Fatal("unbounded calendar reply was accepted")
	}
}
