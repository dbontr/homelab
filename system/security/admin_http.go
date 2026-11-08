package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func readJSON(r *http.Request, value any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("application/json required")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return errors.New("trailing JSON content")
	}
	return nil
}

func respondJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func privateHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

// OwnerHandler must only be reachable through the owner-only Unix socket.
// It never returns a bearer token or the protected TOTP key.
func (a *AdminSessions) OwnerHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/session", func(w http.ResponseWriter, _ *http.Request) { respondJSON(w, 200, a.Status()) })
	mux.HandleFunc("POST /v1/session", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Password string `json:"password"`
			TOTP     string `json:"totp"`
		}
		if err := readJSON(r, &input); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		status, err := a.Authenticate(input.Password, input.TOTP)
		if err != nil {
			log.Print("security audit owner_authentication denied")
			http.Error(w, "authentication denied", 401)
			return
		}
		log.Print("security audit owner_session opened")
		respondJSON(w, 200, status)
	})
	mux.HandleFunc("DELETE /v1/session", func(w http.ResponseWriter, _ *http.Request) {
		a.Revoke()
		log.Print("security audit owner_session revoked")
		respondJSON(w, 200, SessionStatus{Active: false})
	})
	mux.HandleFunc("GET /v1/confirmations", func(w http.ResponseWriter, _ *http.Request) { respondJSON(w, 200, a.Pending()) })
	mux.HandleFunc("POST /v1/confirmations/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Password string `json:"password"`
			TOTP     string `json:"totp"`
		}
		if err := readJSON(r, &input); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if err := a.Confirm(r.PathValue("id"), input.Password, input.TOTP); err != nil {
			log.Print("security audit owner_confirmation denied")
			http.Error(w, "confirmation denied", 403)
			return
		}
		log.Print("security audit owner_confirmation accepted")
		respondJSON(w, 200, map[string]bool{"confirmed": true})
	})

 a.registerCredentialRotation(mux)
 mux.HandleFunc("GET /v1/credentials",func(w http.ResponseWriter,_ *http.Request){
  if a.vault==nil{http.Error(w,"credential vault unavailable",503);return}
  respondJSON(w,200,a.vault.List())
 })
 mux.HandleFunc("POST /v1/credentials",func(w http.ResponseWriter,r *http.Request){
  if a.vault==nil{http.Error(w,"credential vault unavailable",503);return}
  var input struct{
   Provider string `json:"provider"`
   Secret string `json:"secret"`
   Password string `json:"password"`
   TOTP string `json:"totp"`
  }
  if err:=readJSON(r,&input);err!=nil{http.Error(w,"invalid credential request",400);return}
  if err:=a.VerifyOwnerFactors(input.Password,input.TOTP);err!=nil{
   log.Print("security audit credential_authentication denied")
   http.Error(w,"authentication denied",403);return
  }
  secret:=[]byte(input.Secret)
  handle,err:=a.vault.Store(input.Provider,secret)
  for i:=range secret{secret[i]=0}
  if err!=nil{http.Error(w,"invalid credential",400);return}
  log.Print("security audit credential stored")
  respondJSON(w,200,handle)
 })
 mux.HandleFunc("DELETE /v1/credentials/{id}",func(w http.ResponseWriter,r *http.Request){
  if a.vault==nil{http.Error(w,"credential vault unavailable",503);return}
  var input struct{Password string `json:"password"`;TOTP string `json:"totp"`}
  if err:=readJSON(r,&input);err!=nil{http.Error(w,"invalid request",400);return}
  if err:=a.VerifyOwnerFactors(input.Password,input.TOTP);err!=nil{
   log.Print("security audit credential_authentication denied")
   http.Error(w,"authentication denied",403);return
  }
  if err:=a.vault.Delete(r.PathValue("id"));err!=nil{http.Error(w,"credential handle unavailable",404);return}
  log.Print("security audit credential revoked")
  respondJSON(w,200,map[string]bool{"revoked":true})
 })
	return privateHeaders(mux)
}

// ClientHandler grants decisions only for fixed, administrator-configured
// operation/resource pairs. A session ID is never passed to the gateway.
// ClientHandler accepts only cryptographically verified owner assertions.
// A gateway process cannot impersonate an owner by sending identity text.
func (a *AdminSessions) ClientHandler() http.Handler {
 mux:=http.NewServeMux()
 authenticated:=func(w http.ResponseWriter,r *http.Request)(string,bool){
  principal,err:=a.VerifiedGatewayPrincipal(r.Header.Get("Cf-Access-Jwt-Assertion"))
  if err!=nil{http.Error(w,"independent owner identity required",http.StatusForbidden);return "",false}
  return principal,true
 }
 mux.HandleFunc("GET /v1/session",func(w http.ResponseWriter,r *http.Request){
  principal,ok:=authenticated(w,r);if !ok{return}
  respondJSON(w,200,a.StatusFor(principal))
 })
 mux.HandleFunc("DELETE /v1/session",func(w http.ResponseWriter,r *http.Request){
  principal,ok:=authenticated(w,r);if !ok{return}
  if !a.RevokeFor(principal){http.Error(w,"owner session unavailable",http.StatusForbidden);return}
  log.Print("security audit owner_session revoked")
  respondJSON(w,200,SessionStatus{Active:false})
 })
 mux.HandleFunc("POST /v1/preview",func(w http.ResponseWriter,r *http.Request){
  principal,ok:=authenticated(w,r);if !ok{return}
  var input struct{
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments json.RawMessage `json:"arguments"`
  }
  if err:=readJSON(r,&input);err!=nil{http.Error(w,"invalid request",400);return}
  respondJSON(w,200,a.PreviewFor(principal,a.client,input.Operation,input.Target,input.Arguments))
 })
 mux.HandleFunc("POST /v1/authorize",func(w http.ResponseWriter,r *http.Request){
  principal,ok:=authenticated(w,r);if !ok{return}
  var input struct{
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments json.RawMessage `json:"arguments"`
  }
  if err:=readJSON(r,&input);err!=nil{http.Error(w,"invalid request",400);return}
  respondJSON(w,200,a.AuthorizeFor(principal,a.client,input.Operation,input.Target,input.Arguments))
 })
 return privateHeaders(mux)
}

// ServeSockets starts a private owner channel and a separate gateway channel.
// Only the gateway channel is group-accessible. No TCP listener is created.
func (a *AdminSessions) ServeSockets(ctx context.Context, ownerPath, clientPath string, clientGroupID int) error {
	if clientGroupID <= 0 || !filepath.IsAbs(ownerPath) || !filepath.IsAbs(clientPath) ||
		filepath.Dir(ownerPath) != filepath.Dir(clientPath) || ownerPath == clientPath ||
		strings.ContainsRune(ownerPath, 0) || strings.ContainsRune(clientPath, 0) {
		return errors.New("invalid socket configuration")
	}
	dir := filepath.Dir(ownerPath)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0007 != 0 {
		return errors.New("insecure security runtime directory")
	}
	if err := os.Chown(dir, -1, clientGroupID); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0750); err != nil {
		return err
	}
	listen := func(path string, mode os.FileMode) (net.Listener, error) {
		if _, err := os.Lstat(path); err == nil {
			return nil, errors.New("security socket already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		listener, err := net.Listen("unix", path)
		if err != nil {
			return nil, err
		}
		if err := os.Chown(path, -1, clientGroupID); err != nil {
			_ = listener.Close()
			return nil, err
		}
		if err := os.Chmod(path, mode); err != nil {
			_ = listener.Close()
			return nil, err
		}
		return listener, nil
	}
	owner, err := listen(ownerPath, 0600)
	if err != nil {
		return err
	}
	defer owner.Close()
	defer os.Remove(ownerPath)
	client, err := listen(clientPath, 0660)
	if err != nil {
		return err
	}
	defer client.Close()
	defer os.Remove(clientPath)
	ownerHTTP := &http.Server{Handler: a.OwnerHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	clientHTTP := &http.Server{Handler: a.ClientHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	var once sync.Once
	finish := make(chan struct{})
	var wg sync.WaitGroup
	var serveErr error
	go func() {
		select {
		case <-ctx.Done():
			_ = ownerHTTP.Close()
			_ = clientHTTP.Close()
		case <-finish:
		}
	}()
	serve := func(server *http.Server, listener net.Listener) {
		defer wg.Done()
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			once.Do(func() { serveErr = err; _ = ownerHTTP.Close(); _ = clientHTTP.Close() })
		}
	}
	wg.Add(2)
	go serve(ownerHTTP, owner)
	go serve(clientHTTP, client)
	wg.Wait()
	close(finish)
	return serveErr
}

// AdminClient communicates with the independent service through a local
// peer-permission-protected socket; it holds no login or executor credential.
type AdminClient struct {
	socket string
	http   *http.Client
}

func NewAdminClient(socket string) (*AdminClient, error) {
	if !filepath.IsAbs(socket) {
		return nil, errors.New("security socket path must be absolute")
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socket)
	}}
	return &AdminClient{socket: socket, http: &http.Client{Transport: transport, Timeout: 3 * time.Second}}, nil
}
func (c *AdminClient) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, body)
	if err != nil {
		return err
	}
 if assertion,ok:=ctx.Value(accessAssertionContextKey{}).(string);ok&&assertion!=""{
  req.Header.Set("Cf-Access-Jwt-Assertion",assertion)
 }
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("security service unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("security service status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(output)
}
// Preview checks policy without consuming a confirmation or renewing the session.
func (c *AdminClient) Preview(ctx context.Context,operation,target string,args json.RawMessage)(Authorization,error){
 var result Authorization
 err:=c.request(ctx,http.MethodPost,"/v1/preview",map[string]any{"operation":operation,"target":target,"arguments":args},&result)
 return result,err
}
func (c *AdminClient) Authorize(ctx context.Context, operation, target string, args json.RawMessage) (Authorization, error) {
	var result Authorization
	err := c.request(ctx, http.MethodPost, "/v1/authorize", map[string]any{"operation": operation, "target": target, "arguments": args}, &result)
	return result, err
}
func (c *AdminClient) Status(ctx context.Context) (SessionStatus, error) {
	var status SessionStatus
	err := c.request(ctx, http.MethodGet, "/v1/session", nil, &status)
	return status, err
}
func (c *AdminClient) Revoke(ctx context.Context) error {
	var status SessionStatus
	return c.request(ctx, http.MethodDelete, "/v1/session", nil, &status)
}

// WithAccessAssertion keeps the signed identity out of tool parameters and
// forwards it only to the independent security service for verification.
type accessAssertionContextKey struct{}
func WithAccessAssertion(ctx context.Context,assertion string)context.Context{
 return context.WithValue(ctx,accessAssertionContextKey{},assertion)
}
func AccessAssertion(ctx context.Context)string{
 assertion,_:=ctx.Value(accessAssertionContextKey{}).(string)
 return assertion
}
