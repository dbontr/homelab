package transport

import (
    "net/http"
    "crypto/sha256"
    "encoding/hex"
    "encoding/base64"
    "net/http/httptest"
    "encoding/json"
    "path/filepath"
    "strings"
    "github.com/dbontr/homelab-new/system/store"
    "testing"
)

func TestLearningAttachmentRoutesAreBoundedByOwnerAndIdentity(t *testing.T) {
    for _, path := range []string{
        "/v1/learning/attachments/session/abc-123",
        "/v1/learning/attachments/problem/p_123",
        "/v1/learning/attachments/source/ABC",
        "/v1/learning/attachment-originals/1a2b3c",
    } {
        if !ownerLearningAttachmentReadPath(path) {
            t.Errorf("valid attachment inspection path rejected: %s",path)
        }
    }
    for _, path := range []string{
        "/v1/learning/attachments/../abc",
        "/v1/learning/attachments/health/abc",
        "/v1/learning/attachments/session/../../private",
        "/v1/learning/attachments/session/",
        "/v1/learning/attachments/session/abc/original",
        "/v1/learning/attachment-originals/abc/extra",
        "/v1/learning/attachment-originals/%2E%2E",
        "/v1/learning/app-tools/get_attachment/call",
    } {
        if ownerLearningAttachmentReadPath(path) {
            t.Errorf("unsafe attachment URL authorized: %s",path)
        }
    }
}

func TestLearningAttachmentFilenameRejectsTraversalAndHeaders(t *testing.T) {
    for _, name := range []string{"", "%2E%2E", "a%2Fb.png", "a%5Cb.png",
        "%00evil.png", "%0Aset-identity%3Aevil", "bad%ZZ"} {
        if _,err:=learningAttachmentFilename(name);err==nil{
            t.Errorf("unsafe file name accepted: %s",name)
        }
    }
    name,err:=learningAttachmentFilename("%E6%95%B0%E5%AD%A6%20notes.pdf")
    if err!=nil||name!="数学 notes.pdf"{t.Fatalf("UTF-8 filename lost: %q %v",name,err)}
}

func TestLearningAttachmentRejectsInvalidInputBeforeForwarding(t *testing.T) {
    service:=&Server{}
    for _,tc:=range []struct{
        mime,kind,id,filename,body string
        status int
    }{
        {"application/octet-stream","session","abc","work.pdf","fake",415},
        {"image/png","project","abc","work.png","fake",400},
        {"image/png","session","bad/id","work.png","fake",400},
        {"image/png","session","abc","%2E%2E","fake",400},
        {"image/png","session","abc","work.png","",400},
    } {
        req:=httptest.NewRequest(http.MethodPost,"/v1/learning/attachments",strings.NewReader(tc.body))
        req.Header.Set("Content-Type",tc.mime)
        req.Header.Set("X-Learning-Owner-Kind",tc.kind)
        req.Header.Set("X-Learning-Owner-ID",tc.id)
        req.Header.Set("X-Learning-Filename",tc.filename)
        result:=httptest.NewRecorder()
        service.uiLearningAttachmentUpload(result,req)
        if result.Code!=tc.status {
            t.Errorf("invalid upload (%s, %s) returned %d, want %d",
                tc.mime,tc.kind,result.Code,tc.status)
        }
    }
    req:=httptest.NewRequest(http.MethodPost,"/v1/learning/attachments",
        strings.NewReader(strings.Repeat("a",learningBrowserAttachmentLimit+1)))
    req.Header.Set("Content-Type","text/plain")
    req.Header.Set("X-Learning-Owner-Kind","session")
    req.Header.Set("X-Learning-Owner-ID","abc")
    req.Header.Set("X-Learning-Filename","document.txt")
    result:=httptest.NewRecorder()
    service.uiLearningAttachmentUpload(result,req)
    if result.Code!=http.StatusRequestEntityTooLarge {
        t.Fatalf("oversized upload returned %d",result.Code)
    }
}


func TestLearningOriginalsRequireOwnerSessionCSRFAndApprovedMedia(t *testing.T) {
    db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
    if err!=nil{t.Fatal(err)}
    service,err:=New(db,strings.Repeat("p",40),nil)
    if err!=nil{t.Fatal(err)}
    manager, accessAssertion := accessTransportFixture(t)
    service.SetSessions(manager)
    if err:=service.SetAllowedOrigins([]string{"https://console.example"});err!=nil{t.Fatal(err)}
    handler:=service.Handler()
    call:=func(method,path,mimeType,body string,identity *accessTestIdentity,csrf string)*httptest.ResponseRecorder{
        req:=httptest.NewRequest(method,path,strings.NewReader(body))
        req.Host="console.example"
        req.Header.Set("Origin","https://console.example")
        if mimeType!=""{req.Header.Set("Content-Type",mimeType)}
        if identity!=nil{req.Header.Set("Cf-Access-Jwt-Assertion",identity.Value)}
        if csrf!=""{req.Header.Set("X-CSRF-Token",csrf)}
        req.Header.Set("X-Learning-Owner-Kind","session")
        req.Header.Set("X-Learning-Owner-ID","abc")
        req.Header.Set("X-Learning-Filename","notes.txt")
        result:=httptest.NewRecorder()
        handler.ServeHTTP(result,req)
        return result
    }
    if got:=call("POST","/v1/learning/attachments","text/plain","original text",nil,"");got.Code!=401{
        t.Fatalf("anonymous original accepted: %d",got.Code)
    }
    identity := &accessTestIdentity{Value: accessAssertion}
    status:=call("GET","/ui/session","","",identity,"")
    var identity struct{CSRF string `json:"csrf_token"`}
    if err:=json.Unmarshal(status.Body.Bytes(),&identity);err!=nil||identity.CSRF==""{
        t.Fatalf("CSRF unavailable: %v",err)
    }
    if got:=call("POST","/v1/learning/attachments","text/plain","original text",identity,"");got.Code!=401{
        t.Fatalf("missing CSRF authorized original: %d",got.Code)
    }
    if got:=call("POST","/v1/learning/attachments","application/javascript","alert(1)",identity,identity.CSRF);got.Code!=401{
        t.Fatalf("unsafe content type passed owner authorization: %d",got.Code)
    }
    if got:=call("POST","/v1/learning/attachments","text/plain","original text",identity,identity.CSRF);got.Code!=502{
        t.Fatalf("allowed attachment did not reach unavailable Learning backend: %d",got.Code)
    }
    if got:=call("GET","/v1/learning/attachments/session/abc","","",identity,"");got.Code!=502{
        t.Fatalf("allowed attachment listing route inaccessible: %d",got.Code)
    }
    if got:=call("GET","/v1/learning/attachment-originals/abc","","",identity,"");got.Code!=502{
        t.Fatalf("original-download route blocked: %d",got.Code)
    }
    if got:=call("GET","/v1/learning/attachment-originals/abc","","",nil,"");got.Code!=401{
        t.Fatalf("anonymous original retrieval accepted: %d",got.Code)
    }
}


func TestLearningOriginalDownloadVerifiesBytesAndSafeMetadata(t *testing.T) {
    data:=[]byte("%PDF-1.4\nowned learning source\n")
    sum:=sha256.Sum256(data)
    original:=map[string]any{
        "base64":base64.StdEncoding.EncodeToString(data),
        "size_bytes":float64(len(data)),
        "sha256":hex.EncodeToString(sum[:]),
        "integrity_verified":true,
        "filename":"research.pdf",
        "mime_type":"application/pdf",
    }
    body,mimeType,filename,err:=checkedLearningOriginal(original)
    if err!=nil||string(body)!=string(data)||mimeType!="application/pdf"||filename!="research.pdf"{
        t.Fatalf("valid original was rejected: %v",err)
    }
    tests:=[]struct{
        field string
        value any
    }{
        {"sha256",strings.Repeat("0",64)},
        {"integrity_verified",false},
        {"mime_type","text/html"},
        {"filename","../data.pdf"},
        {"filename","evil\nresponse.pdf"},
        {"size_bytes",float64(len(data)+1)},
        {"base64","%%%%"},
    }
    for _,test:=range tests{
        item:=map[string]any{}
        for key,value:=range original{item[key]=value}
        item[test.field]=test.value
        if _,_,_,err:=checkedLearningOriginal(item);err==nil{
            t.Errorf("modified %s bypassed integrity checks",test.field)
        }
    }
}
