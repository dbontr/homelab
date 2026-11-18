package transport

import (
 "bytes"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "image/png"
 "net/http"
 "regexp"
 "strings"
)

var visualArtifactID = regexp.MustCompile("^[a-f0-9]{32}$")
var visualArtifactSHA256 = regexp.MustCompile("^[a-f0-9]{64}$")

// uiVisualArtifact serves an independently authorized, checksum-verified
// project PNG over the existing same-origin, authenticated browser interface.
// It does not grant access to arbitrary host displays or export other files.
func(s *Server)uiVisualArtifact(w http.ResponseWriter,r *http.Request){
 project,id:=r.PathValue("project"),r.PathValue("artifact")
 if !consoleProjectID.MatchString(project)||!visualArtifactID.MatchString(id){
  writeError(w,http.StatusBadRequest,"invalid project image identifier")
  return
 }
 var workspace *Backend
 for i:=range s.backend {
  if s.backend[i].Name=="workspace" {workspace=&s.backend[i];break}
 }
 if workspace==nil {writeError(w,http.StatusServiceUnavailable,"project image service unavailable");return}
 params,_:=json.Marshal(map[string]any{
  "name":"artifact_view_image",
  "arguments":map[string]string{"project_id":project,"artifact_id":id},
 })
 var response any
 err:=s.forward(r.Context(),*workspace,rpcRequest{
  JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/call",Params:params,
 },&response)
 if err!=nil{writeError(w,http.StatusServiceUnavailable,"project image unavailable");return}
 raw,ok:=response.(map[string]any)
 if !ok||raw["isError"]==true{writeError(w,http.StatusNotFound,"project image unavailable");return}
 parts,ok:=raw["content"].([]any)
 if !ok||len(parts)<1{writeError(w,http.StatusBadGateway,"invalid image response");return}
 imagePart,ok:=parts[0].(map[string]any)
 if !ok||imagePart["type"]!="image"||imagePart["mimeType"]!="image/png"{
  writeError(w,http.StatusBadGateway,"invalid PNG image response");return
 }
 structured,ok:=raw["structuredContent"].(map[string]any)
 if !ok||structured["artifact_id"]!=id||structured["mime_type"]!="image/png"{
  writeError(w,http.StatusBadGateway,"image identity mismatch");return
 }
 digest,ok:=structured["sha256"].(string)
 if !ok||!visualArtifactSHA256.MatchString(digest){
  writeError(w,http.StatusBadGateway,"image digest unavailable");return
 }
 encoded,ok:=imagePart["data"].(string)
 if !ok||len(encoded)==0||len(encoded)>base64.StdEncoding.EncodedLen(4<<20){
  writeError(w,http.StatusBadGateway,"image exceeds inline limit");return
 }
 data,err:=base64.StdEncoding.Strict().DecodeString(encoded)
 if err!=nil||len(data)==0||len(data)>4<<20{
  writeError(w,http.StatusBadGateway,"invalid image encoding");return
 }
 sum:=sha256.Sum256(data)
 if !strings.EqualFold(hex.EncodeToString(sum[:]),digest){
  writeError(w,http.StatusBadGateway,"image checksum mismatch");return
 }
 config,err:=png.DecodeConfig(bytes.NewReader(data))
 if err!=nil||config.Width<1||config.Height<1||config.Width>3840||config.Height>2160||
   config.Width*config.Height>8_000_000{
  writeError(w,http.StatusBadGateway,"image dimensions invalid");return
 }
 expectedWidth,okW:=structured["width"].(float64)
 expectedHeight,okH:=structured["height"].(float64)
 if !okW||!okH||int(expectedWidth)!=config.Width||int(expectedHeight)!=config.Height{
  writeError(w,http.StatusBadGateway,"image dimensions mismatch");return
 }
 if _,err:=png.Decode(bytes.NewReader(data));err!=nil{
  writeError(w,http.StatusBadGateway,"image data invalid");return
 }
 w.Header().Set("Content-Type","image/png")
 w.Header().Set("Content-Disposition","inline")
 w.Header().Set("Cache-Control","private, no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("X-Content-SHA256",digest)
 w.WriteHeader(http.StatusOK)
 _,_=w.Write(data)
}

