package health

import(
 "bytes"
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/base64"
 "encoding/hex"
 "errors"
 "fmt"
 "net/http"
 "strings"
 "time"
)

const maxEvidenceBytes=2<<20

type Evidence struct{
 ID string `json:"id"`
 Filename string `json:"filename"`
 MIME string `json:"mime"`
 Bytes int `json:"bytes"`
 SHA256 string `json:"sha256"`
 Source string `json:"source"`
 Description string `json:"description,omitempty"`
 CreatedAt time.Time `json:"created_at"`
}

func(s *Store)initEvidence()error{
 _,err:=s.db.Exec(`CREATE TABLE IF NOT EXISTS evidence(
  id TEXT PRIMARY KEY,filename TEXT NOT NULL,mime TEXT NOT NULL,
  bytes INTEGER NOT NULL,sha256 TEXT NOT NULL,source TEXT NOT NULL,
  description TEXT NOT NULL,created_at TEXT NOT NULL,payload BLOB NOT NULL)`)
 if err!=nil{return fmt.Errorf("evidence schema: %w",err)}
 return nil
}
func sniffEvidence(data []byte)string{
 if len(data)>=12 && bytes.Equal(data[4:8],[]byte("ftyp")){
  brand:=string(data[8:12])
  if strings.HasPrefix(brand,"hei")||brand=="mif1"{return "image/heic"}
 }
 typ:=http.DetectContentType(data)
 for _,allowed:=range []string{"image/jpeg","image/png","image/webp","application/pdf"}{
  if typ==allowed{return typ}
 }
 return ""
}
func(s *Store)AddEvidence(ctx context.Context,filename,expectedMIME,encoded,source,description string)(Evidence,error){
 if strings.TrimSpace(filename)==""||len(filename)>255{return Evidence{},errors.New("evidence filename is required")}
 if strings.ContainsAny(filename,"/\\\x00\r\n"){return Evidence{},errors.New("evidence filename contains forbidden path characters")}
 if source==""||len(source)>128||len(description)>2048{return Evidence{},errors.New("invalid evidence source or description")}
 if len(encoded)>((maxEvidenceBytes+2)/3*4+8){return Evidence{},errors.New("attachment exceeds size limit")}
 data,err:=base64.StdEncoding.Strict().DecodeString(encoded)
 if err!=nil{return Evidence{},errors.New("attachment must be base64 encoded")}
 if len(data)==0||len(data)>maxEvidenceBytes{return Evidence{},errors.New("attachment must be 1 byte to 2 MiB")}
 detected:=sniffEvidence(data)
 if detected==""||detected!=expectedMIME{return Evidence{},errors.New("file type does not match an allowed media type")}
 sum:=sha256.Sum256(data)
 id,err:=newID();if err!=nil{return Evidence{},err}
 e:=Evidence{ID:id,Filename:filename,MIME:detected,Bytes:len(data),SHA256:hex.EncodeToString(sum[:]),
  Source:source,Description:description,CreatedAt:time.Now().UTC()}
 _,err=s.db.ExecContext(ctx,`INSERT INTO evidence(id,filename,mime,bytes,sha256,source,description,created_at,payload)
 VALUES(?,?,?,?,?,?,?,?,?)`,e.ID,e.Filename,e.MIME,e.Bytes,e.SHA256,e.Source,e.Description,
  e.CreatedAt.Format(time.RFC3339Nano),data)
 if err!=nil{return Evidence{},err}
 return e,nil
}
func(s *Store)GetEvidence(ctx context.Context,id string,includeContent bool)(Evidence,string,error){
 var e Evidence;var at string
 var encoded string
 q:=`SELECT id,filename,mime,bytes,sha256,source,description,created_at`
 if includeContent{q+=",payload"}
 q+=" FROM evidence WHERE id=?"
 if includeContent{
  var data []byte
  err:=s.db.QueryRowContext(ctx,q,id).Scan(&e.ID,&e.Filename,&e.MIME,&e.Bytes,&e.SHA256,
   &e.Source,&e.Description,&at,&data)
  if err!=nil{return Evidence{},"",err}
  encoded=base64.StdEncoding.EncodeToString(data)
 }else{
  err:=s.db.QueryRowContext(ctx,q,id).Scan(&e.ID,&e.Filename,&e.MIME,&e.Bytes,&e.SHA256,
   &e.Source,&e.Description,&at)
  if err!=nil{return Evidence{},"",err}
 }
 var err error
 e.CreatedAt,err=time.Parse(time.RFC3339Nano,at)
 return e,encoded,err
}
func(s *Store)ListEvidence(ctx context.Context,limit int)([]Evidence,error){
 if limit<1||limit>100{return nil,errors.New("limit must be between 1 and 100")}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,filename,mime,bytes,sha256,source,description,created_at
 FROM evidence ORDER BY created_at DESC LIMIT ?`,limit)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]Evidence{}
 for rows.Next(){
  var e Evidence;var at string
  if err=rows.Scan(&e.ID,&e.Filename,&e.MIME,&e.Bytes,&e.SHA256,
   &e.Source,&e.Description,&at);err!=nil{return nil,err}
  if e.CreatedAt,err=time.Parse(time.RFC3339Nano,at);err!=nil{return nil,err}
  out=append(out,e)
 }
 return out,rows.Err()
}
func(s *Store)EvidencePresent(ctx context.Context,id string)(bool,error){
 var found string
 err:=s.db.QueryRowContext(ctx,"SELECT id FROM evidence WHERE id=?",id).Scan(&found)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 return err==nil,err
}
