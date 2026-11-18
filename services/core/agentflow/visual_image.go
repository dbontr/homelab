package agentflow

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "errors"
 "image/png"
 "strings"
)

// ProjectPNG is a digest-verified project-scoped image. Image bytes are never
// persisted inside the Engineering workflow or returned through run status.
type ProjectPNG struct {
 ArtifactID string
 SHA256 string
 Width int
 Height int
 Data []byte
}

// ProjectPNGReader is an explicit project-authorization boundary for vision.
type ProjectPNGReader interface {
 ReadProjectPNG(context.Context, string, string) (ProjectPNG, error)
}

func (c *ComputeClient) ReadProjectPNG(ctx context.Context, project, artifactID string) (ProjectPNG, error) {
 if !validID(project) || !validID(artifactID) {
  return ProjectPNG{},errors.New("invalid visual artifact scope")
 }
 response,err:=c.Call(ctx,"artifact_view_image",map[string]any{"project_id":project,"artifact_id":artifactID})
 if err!=nil{return ProjectPNG{},err}
 meta:=obj(response["metadata"])
 if meta==nil||str(meta["artifact_id"])!=artifactID||str(meta["mime_type"])!="image/png"||!hexDigest(str(meta["sha256"])) {
  return ProjectPNG{},errors.New("image metadata does not match the authorized artifact")
 }
 encoded:=str(response["data_base64"])
 if len(encoded)==0||len(encoded)>base64.StdEncoding.EncodedLen(4<<20) {
  return ProjectPNG{},errors.New("visual artifact exceeds image limit")
 }
 data,err:=base64.StdEncoding.Strict().DecodeString(encoded)
 if err!=nil||len(data)==0||len(data)>4<<20{return ProjectPNG{},errors.New("invalid visual PNG encoding")}
 digest:=sha256.Sum256(data)
 actual:=hex.EncodeToString(digest[:])
 if !strings.EqualFold(actual,str(meta["sha256"])) {
  return ProjectPNG{},errors.New("visual image digest mismatch")
 }
 width,okWidth:=meta["width"].(float64)
 height,okHeight:=meta["height"].(float64)
 if !okWidth||!okHeight||width<1||height<1||width>3840||height>2160||width*height>8000000 {
  return ProjectPNG{},errors.New("invalid visual image dimensions")
 }
 configuration,err:=png.DecodeConfig(bytes.NewReader(data))
 if err!=nil||configuration.Width!=int(width)||configuration.Height!=int(height){
  return ProjectPNG{},errors.New("visual PNG dimensions do not match artifact metadata")
 }
 if _,err:=png.Decode(bytes.NewReader(data));err!=nil{return ProjectPNG{},errors.New("corrupt visual PNG")}
 return ProjectPNG{ArtifactID:artifactID,SHA256:actual,Width:int(width),Height:int(height),Data:data},nil
}
