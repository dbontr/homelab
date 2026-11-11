package transport

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const learningBrowserAttachmentLimit = 5 << 20

var learningAttachmentMIME = map[string]bool{
	"image/jpeg":true,
	"image/png":true,
	"image/webp":true,
	"image/heic":true,
	"application/pdf":true,
	"text/plain":true,
	"text/markdown":true,
}

func ownerLearningAttachmentReadPath(path string) bool {
	if strings.HasPrefix(path,"/v1/learning/attachment-originals/") {
		id:=strings.TrimPrefix(path,"/v1/learning/attachment-originals/")
		return learningID.MatchString(id)
	}
	parts:=strings.Split(path,"/")
	return len(parts)==6 && parts[1]=="v1" && parts[2]=="learning" &&
		parts[3]=="attachments" && validLearningAttachmentOwner(parts[4]) &&
		learningID.MatchString(parts[5])
}

func validLearningAttachmentOwner(kind string) bool {
	return kind=="problem" || kind=="session" || kind=="source"
}

func learningAttachmentFilename(encoded string) (string,error) {
	if len(encoded)==0||len(encoded)>750 {return "",errors.New("invalid original filename")}
	name,err:=url.QueryUnescape(encoded)
	if err!=nil||!utf8.ValidString(name)||len(name)==0||len(name)>250||
		name=="."||name==".."||strings.ContainsAny(name,"/\\\x00\r\n"){
		return "",errors.New("invalid original filename")
	}
	return name,nil
}

// Attachment calls use an exact Learning tool identity and the private
// upstream token. General MCP arguments keep their separate 1 MiB limit.
func (s *Server) learningAttachmentTool(ctx context.Context,name string,args any)(map[string]any,error){
	var backend *Backend
	for i:=range s.backend{
		if s.backend[i].Name=="learning"{
			backend=&s.backend[i]
			break
		}
	}
	if backend==nil{return nil,errors.New("Learning backend unavailable")}
	params,err:=json.Marshal(map[string]any{"name":name,"arguments":args})
	if err!=nil{return nil,errors.New("cannot encode Learning arguments")}
	var reply any
	if err=s.forward(ctx,*backend,rpcRequest{
		JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/call",Params:params,
	},&reply);err!=nil{return nil,err}
	obj,ok:=reply.(map[string]any)
	if !ok{return nil,errors.New("invalid Learning tool response")}
	if failed,_:=obj["isError"].(bool);failed{
		return nil,errors.New("Learning rejected the attachment operation")
	}
	blocks,ok:=obj["content"].([]any)
	if !ok||len(blocks)!=1{return nil,errors.New("invalid Learning tool content")}
	first,ok:=blocks[0].(map[string]any)
	if !ok{return nil,errors.New("invalid Learning tool content")}
	text,ok:=first["text"].(string)
	if !ok||len(text)>7<<20{return nil,errors.New("Learning attachment response too large")}
	var record map[string]any
	if err=json.Unmarshal([]byte(text),&record);err!=nil||record==nil{
		return nil,errors.New("Learning returned invalid attachment JSON")
	}
	return record,nil
}

func (s *Server) uiLearningAttachmentList(w http.ResponseWriter,r *http.Request){
	kind,id:=r.PathValue("kind"),r.PathValue("id")
	if !validLearningAttachmentOwner(kind)||!learningID.MatchString(id){
		writeError(w,http.StatusBadRequest,"invalid attachment owner")
		return
	}
	s.learningCall(w,r,"list_attachments",map[string]string{
		"owner_kind":kind,"owner_id":id,
	},http.StatusOK)
}

func (s *Server) uiLearningAttachmentUpload(w http.ResponseWriter,r *http.Request){
	kind:=r.Header.Get("X-Learning-Owner-Kind")
	id:=r.Header.Get("X-Learning-Owner-ID")
	filename,err:=learningAttachmentFilename(r.Header.Get("X-Learning-Filename"))
	if !validLearningAttachmentOwner(kind)||!learningID.MatchString(id)||err!=nil{
		writeError(w,http.StatusBadRequest,"invalid attachment identity")
		return
	}
	mediaType,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err!=nil||!learningAttachmentMIME[mediaType]{
		writeError(w,http.StatusUnsupportedMediaType,"unsupported attachment type")
		return
	}
	if r.ContentLength>learningBrowserAttachmentLimit{
		writeError(w,http.StatusRequestEntityTooLarge,"attachment exceeds 5 MiB")
		return
	}
	r.Body=http.MaxBytesReader(w,r.Body,learningBrowserAttachmentLimit+1)
	body,err:=io.ReadAll(r.Body)
	if err!=nil||len(body)>learningBrowserAttachmentLimit{
		writeError(w,http.StatusRequestEntityTooLarge,"attachment exceeds 5 MiB")
		return
	}
	if len(body)==0{
		writeError(w,http.StatusBadRequest,"original attachment is empty")
		return
	}
	result,err:=s.learningAttachmentTool(r.Context(),"upload_attachment",map[string]string{
		"owner_kind":kind,"owner_id":id,"filename":filename,
		"mime_type":mediaType,"base64":base64.StdEncoding.EncodeToString(body),
	})
	if err!=nil{
		writeError(w,http.StatusBadGateway,"Learning could not store this attachment")
		return
	}
	// Only the verified record metadata is returned; raw bytes never become
	// part of this HTTP response.
	delete(result,"base64")
	writeJSON(w,http.StatusCreated,result)
}

func (s *Server) uiLearningAttachmentOriginal(w http.ResponseWriter,r *http.Request){
	id:=r.PathValue("id")
	if !learningID.MatchString(id){
		writeError(w,http.StatusBadRequest,"invalid attachment ID")
		return
	}
	record,err:=s.learningAttachmentTool(r.Context(),"get_attachment",map[string]any{
		"attachment_id":id,"include_original":true,
	})
	if err!=nil{
		writeError(w,http.StatusBadGateway,"Learning could not retrieve the original")
		return
	}
	encoded,ok:=record["base64"].(string)
	if !ok||len(encoded)>((learningBrowserAttachmentLimit+2)/3)*4+16{
		writeError(w,http.StatusBadGateway,"original exceeds the browser transfer limit")
		return
	}
	body,err:=base64.StdEncoding.Strict().DecodeString(encoded)
	if err!=nil||len(body)==0||len(body)>learningBrowserAttachmentLimit{
		writeError(w,http.StatusBadGateway,"invalid original attachment")
		return
	}
	expected,ok:=record["sha256"].(string)
	digest:=sha256.Sum256(body)
	if !ok||hex.EncodeToString(digest[:])!=expected||
		record["integrity_verified"]!=true{
		writeError(w,http.StatusBadGateway,"original attachment integrity could not be verified")
		return
	}
	mediaType,ok:=record["mime_type"].(string)
	if !ok||!learningAttachmentMIME[mediaType]{
		writeError(w,http.StatusBadGateway,"original has unsupported MIME type")
		return
	}
	filename,ok:=record["filename"].(string)
	if !ok||len(filename)==0||len(filename)>250{
		writeError(w,http.StatusBadGateway,"original filename unavailable")
		return
	}
	w.Header().Set("Content-Type",mediaType)
	w.Header().Set("Content-Disposition",mime.FormatMediaType("attachment",map[string]string{"filename":filename}))
	w.Header().Set("X-Content-Type-Options","nosniff")
	w.Header().Set("Cache-Control","private, no-store")
	w.Header().Set("Content-Security-Policy","sandbox")
	w.WriteHeader(http.StatusOK)
	_,_=w.Write(body)
}
