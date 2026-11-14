// Package mcp exposes health records and RecipeSage through the Model Context Protocol.
package mcp

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/dbontr/homelab-new/system/health/internal/recipesage"
 "github.com/dbontr/homelab-new/system/health/internal/health"
 "github.com/dbontr/homelab-new/system/health/internal/googlehealth"
 "github.com/dbontr/homelab-new/system/health/internal/fooddata"
)

const protocolVersion = "2025-06-18"
const version = "0.1.0"
const maxMessage = 4 << 20

type Server struct {
	Access *AccessVerifier
	client *recipesage.Client
 health *health.Store
 google *googlehealth.Client
 food *fooddata.Client
	tools  map[string]toolEntry
 bundle *AppBundle
}

func New(client *recipesage.Client, stores ...*health.Store) *Server {
	s := &Server{client: client, tools: map[string]toolEntry{}}
	if client != nil { s.registerTools() }
 if len(stores)>0 && stores[0]!=nil {s.health=stores[0];s.registerHealthTools();s.registerJournalTools();s.registerMedicationTools();s.registerAllergyTools();s.registerLaboratoryTools();s.registerTrainingTools();s.registerGroceryTools();s.registerNutrientCatalog();s.registerFoodProposalTools();s.registerPortabilityTools();s.registerEvidenceTools();s.registerSeriesTools();s.registerExperimentTools();s.registerResearchTools();s.registerTechniqueTools();s.registerPhysiologyTools();s.registerAnalyticTools();s.registerRescheduling();s.registerWorkoutEditor();s.registerWorkoutCompanion();s.registerGroceryEditor();s.registerCognitiveTest();s.registerDashboard()}
	return s
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcErr(id json.RawMessage, code int, msg string) *response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// Dispatch returns nil for JSON-RPC notifications.
func (s *Server) Dispatch(ctx context.Context, data []byte) *response {
	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		return rpcErr(nil, -32700, "invalid JSON")
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return rpcErr(req.ID, -32600, "invalid JSON-RPC request")
	}
	if len(req.ID) == 0 {
		return nil
	}
	switch req.Method {
	case "initialize":
		return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}, "resources": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "personal-health-mcp", "version": version},
			"instructions":    "Record health metrics and manage workout plans, food logs, goals, and connected recipes. Distinguish estimates from measurements. Never infer that a workout was missed solely from an absent log.",
		}}
	case "ping":
		return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "resources/list":
  return &response{JSONRPC:"2.0",ID:req.ID,Result:map[string]any{"resources":[]any{
   map[string]any{"uri":healthUIURI,"name":"Health workout planner","mimeType":"text/html;profile=mcp-app","description":"Interactive workout planning workspace"},
   map[string]any{"uri":cognitiveUIURI,"name":"Visual reaction-time test","mimeType":"text/html;profile=mcp-app","description":"Interactive response-timing task"},
   map[string]any{"uri":dashboardURI,"name":"Health dashboard","mimeType":"text/html;profile=mcp-app","description":"Interactive health measurements and nutrient targets"},
   map[string]any{"uri":groceryUIURI,"name":"Grocery planner","mimeType":"text/html;profile=mcp-app","description":"Interactive grocery and meal-plan editor"},
   map[string]any{"uri":companionUIURI,"name":"Workout companion","mimeType":"text/html;profile=mcp-app","description":"Interactive live workout logging"},
  }}}
 case "resources/read":
  var p struct{URI string `json:"uri"`}
  if json.Unmarshal(req.Params,&p)!=nil {return rpcErr(req.ID,-32602,"invalid resource request")}
  switch p.URI {
  case healthUIURI:
  return &response{JSONRPC:"2.0",ID:req.ID,Result:map[string]any{"contents":[]any{
   s.appResource(healthUIURI,"workout-editor",workoutHTML),
  }}}
  case cognitiveUIURI:
  return &response{JSONRPC:"2.0",ID:req.ID,Result:map[string]any{"contents":[]any{
   s.appResource(cognitiveUIURI,"cognitive",cognitiveHTML),
  }}}
  case dashboardURI:
  return &response{JSONRPC:"2.0",ID:req.ID,Result:map[string]any{"contents":[]any{
   s.appResource(dashboardURI,"dashboard",dashboardHTML),
  }}}
  case groceryUIURI:
  return &response{JSONRPC:"2.0",ID:req.ID,Result:map[string]any{"contents":[]any{
   s.appResource(groceryUIURI,"groceries",groceryHTML),
  }}}
  case companionUIURI:
  return &response{JSONRPC:"2.0",ID:req.ID,Result:map[string]any{"contents":[]any{
   s.appResource(companionUIURI,"workout-companion",companionHTML),
  }}}
  default:
    return rpcErr(req.ID,-32602,"unknown resource")
  }
 case "tools/list":
		list := make([]Tool, 0, len(s.tools))
		for _, entry := range s.tools {
			list = append(list, entry.Tool)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": list}}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return rpcErr(req.ID, -32602, "tool name is required")
		}
		entry, ok := s.tools[p.Name]
		if !ok {
			return rpcErr(req.ID, -32602, "unknown tool")
		}
		args := map[string]json.RawMessage{}
		if len(p.Arguments) > 0 && string(p.Arguments) != "null" {
			if err := json.Unmarshal(p.Arguments, &args); err != nil || args == nil {
				return rpcErr(req.ID, -32602, "tool arguments must be an object")
			}
		}
		result, err := entry.run(ctx, args)
		if err != nil {
			return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"content": []any{map[string]string{"type": "text", "text": err.Error()}}, "isError": true}}
		}
		if len(result) == 0 {
			result = json.RawMessage(`{}`)
		}
		return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"content": []any{map[string]string{"type": "text", "text": string(result)}}, "isError": false}}
	default:
		return rpcErr(req.ID, -32601, "method not found")
	}
}

// ServeStdio implements the newline-delimited JSON-RPC MCP transport.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), maxMessage)
	writer := bufio.NewWriter(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp := s.Dispatch(ctx, scanner.Bytes())
		if resp == nil {
			continue
		}
		encoded, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if _, err = writer.Write(append(encoded, '\n')); err != nil {
			return err
		}
		if err = writer.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// HTTPHandler provides stateless Streamable HTTP with JSON responses.
// The caller must terminate TLS at the reverse proxy for non-local access.
func (s *Server) HTTPHandler(bearer string, allowedOrigins []string) (http.Handler, error) {
	if bearer == "" && s.Access == nil {
		return nil, errors.New("HTTP mode requires a bearer token or verified Cloudflare Access")
	}
	origins := map[string]bool{}
	for _, origin := range allowedOrigins {
		if origin != "" {
			origins[origin] = true
		}
	}
	mux := http.NewServeMux()
	if s.bundle != nil {
		mux.HandleFunc("/",s.bundle.serve)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" {
			if !origins[origin] {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		header := r.Header.Get("Authorization")
		provided, ok := strings.CutPrefix(header, "Bearer ")
		authorized := bearer != "" && ok && subtle.ConstantTimeCompare([]byte(provided), []byte(bearer)) == 1
		if !authorized && s.Access != nil {
			authorized = s.Access.Verify(r.Header.Get("Cf-Access-Jwt-Assertion")) == nil
		}
		if !authorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if typ := r.Header.Get("Content-Type"); !strings.HasPrefix(typ, "application/json") {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		if accept := r.Header.Get("Accept"); accept != "" && !strings.Contains(accept, "application/json") && !strings.Contains(accept, "*/*") {
			http.Error(w, "Accept must allow application/json", http.StatusNotAcceptable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxMessage)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "request exceeds size limit or cannot be read", http.StatusRequestEntityTooLarge)
			return
		}
		resp := s.Dispatch(r.Context(), data)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err = json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("write MCP response: %v", err)
		}
	})
	return mux, nil
}

func (s *Server) ListenHTTP(ctx context.Context, address, token string, origins []string) error {
	handler, err := s.HTTPHandler(token, origins)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5_000_000_000, MaxHeaderBytes: 16 << 10}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()
	log.Printf("Personal Health MCP listening on %s", address)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("listen: %w", err)
}
