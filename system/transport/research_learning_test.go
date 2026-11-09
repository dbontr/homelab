package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dbontr/homelab-new/system/store"
)

type studyTransferBackend struct {
	mu sync.Mutex
	imports int
	corrupt bool
}

func (b *studyTransferBackend) ServeHTTP(w http.ResponseWriter,r *http.Request) {
	var call struct{
		ID json.RawMessage `json:"id"`
		Method string `json:"method"`
		Params struct{
			Name string `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if json.NewDecoder(r.Body).Decode(&call)!=nil{http.Error(w,"invalid JSON",400);return}
	var result any
	switch call.Method {
	case "tools/list":
		result=map[string]any{"tools":[]any{
			map[string]any{"name":"study_pack_prepare","inputSchema":map[string]any{"type":"object"}},
			map[string]any{"name":"ingest_source_text","inputSchema":map[string]any{"type":"object"}},
		}}
	case "tools/call":
		var data any
		switch call.Params.Name {
		case "study_pack_prepare":
			body:="Exact source evidence"
			sum:=sha256.Sum256([]byte(body))
			b.mu.Lock()
			if b.corrupt{body="Tampered source evidence"}
			b.mu.Unlock()
			pack:=map[string]any{
				"project":"study","title":"Transform theory",
				"objective":"Understand source evidence",
				"stored":false,"source_verified":true,
				"questions":[]string{},
				"passages":[]map[string]any{{
					"citation_ref":"source:proof@1:1","source_id":"proof","title":"Research",
					"original":"source.txt","original_sha256":strings.Repeat("a",64),
					"repository_revision":"abc123","page":1,"section":"Introduction",
					"line_start":1,"line_end":5,"body":body,
					"body_sha256":hex.EncodeToString(sum[:]),
				}},
			}
			raw,_:=json.Marshal(pack)
			digest:=sha256.Sum256(raw)
			pack["sha256"]=hex.EncodeToString(digest[:])
			data=pack
		case "ingest_source_text":
			text,_:=call.Params.Arguments["text"].(string)
			if !strings.Contains(text,"source:proof@1:1") ||
				!strings.Contains(text,"Exact source evidence") {
				http.Error(w,"missing source evidence",400);return
			}
			b.mu.Lock()
			b.imports++
			b.mu.Unlock()
			data=map[string]any{"id":"learning-source","duplicate":false,"spans":1}
		default:
			http.Error(w,"unsupported tool",400);return
		}
		raw,_:=json.Marshal(data)
		result=map[string]any{"content":[]any{map[string]any{"type":"text","text":string(raw)}}}
	default:
		http.Error(w,"unknown method",400);return
	}
	w.Header().Set("Content-Type","application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc":"2.0","id":call.ID,"result":result})
}

func TestResearchLearningRequiresExactVerifiedPreview(t *testing.T){
	b:=&studyTransferBackend{}
	workspace:=httptest.NewServer(b)
	learning:=httptest.NewServer(b)
	defer workspace.Close()
	defer learning.Close()
	db,err:=store.Open(filepath.Join(t.TempDir(),"records.json"),"UTC")
	if err!=nil{t.Fatal(err)}
	server,err:=New(db,strings.Repeat("z",40),[]Backend{
		{Name:"workspace",URL:workspace.URL},{Name:"learning",URL:learning.URL},
	})
	if err!=nil{t.Fatal(err)}
	h:=server.Handler()
	in:=ResearchLearningRequest{
		ProjectID:"study",Title:"Transform theory",
		Objective:"Understand source evidence",PassageIDs:[]string{"passage-id"},
	}
	raw,_:=json.Marshal(in)
	path:="/v1/research/learning/preview"
	if r:=request(h,"POST",path,string(raw),"");r.Code!=401{
		t.Fatalf("anonymous preview accepted: %d",r.Code)
	}
	preview:=request(h,"POST",path,string(raw),strings.Repeat("z",40))
	if preview.Code!=200{t.Fatalf("preview failed: %d %s",preview.Code,preview.Body.String())}
	var verified struct{PackSHA256 string `json:"pack_sha256"`; Passages int `json:"passages"`}
	if err:=json.Unmarshal(preview.Body.Bytes(),&verified);err!=nil{t.Fatal(err)}
	if verified.Passages!=1 || len(verified.PackSHA256)!=64{t.Fatalf("bad preview: %+v",verified)}

	importPath:="/v1/research/learning/import"
	if r:=request(h,"POST",importPath,string(raw),strings.Repeat("z",40));r.Code!=400{
		t.Fatalf("unconfirmed import accepted: %d",r.Code)
	}
	in.Confirm=true
	in.ExpectedSHA256=strings.Repeat("0",64)
	raw,_=json.Marshal(in)
	if r:=request(h,"POST",importPath,string(raw),strings.Repeat("z",40));r.Code!=400{
		t.Fatalf("stale digest accepted: %d",r.Code)
	}
	b.mu.Lock()
	if b.imports!=0{t.Fatal("learning source was stored without approval")}
	b.mu.Unlock()

	in.ExpectedSHA256=verified.PackSHA256
	raw,_=json.Marshal(in)
	done:=request(h,"POST",importPath,string(raw),strings.Repeat("z",40))
	if done.Code!=201 || !strings.Contains(done.Body.String(),`"mastery_awarded":false`){
		t.Fatalf("confirmed import failed: %d %s",done.Code,done.Body.String())
	}
	b.mu.Lock()
	imports:=b.imports
	b.corrupt=true
	b.mu.Unlock()
	if imports!=1{t.Fatalf("expected one explicit import, got %d",imports)}
	if r:=request(h,"POST",path,string(raw),strings.Repeat("z",40));r.Code!=400{
		t.Fatalf("corrupted passage was accepted: %d %s",r.Code,r.Body.String())
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.imports!=1{t.Fatal("corrupted source was imported")}
}

func TestResearchLearningRejectsInvalidSourceScope(t *testing.T){
	for _,invalid:=range []ResearchLearningRequest{
		{ProjectID:"../../etc",Title:"Bad",PassageIDs:[]string{"passage"}},
		{ProjectID:"study",Title:"",PassageIDs:[]string{"passage"}},
		{ProjectID:"study",Title:"Good",PassageIDs:[]string{"passage","passage"}},
		{ProjectID:"study",Title:"Good",PassageIDs:[]string{"../secret"}},
	}{
		if validateResearchLearningRequest(invalid)==nil{
			t.Fatalf("invalid research scope accepted: %+v",invalid)
		}
	}
}
