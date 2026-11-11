package transport

import (
    "net/http"
    "net/http/httptest"
    "strings"
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
        "%00evil.png", "%0Aset-cookie%3Aevil", "bad%ZZ"} {
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
