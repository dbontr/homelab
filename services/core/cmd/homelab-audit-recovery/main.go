// Root-console reconciliation of host executor audit records. This process
// must never run concurrently with the privileged executor.
package main

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "syscall"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
 "github.com/dbontr/homelab-new/system/privileged"
 "github.com/dbontr/homelab-new/system/statelease"
)

const maxEvidenceBytes int64 = 8192

type evidence struct {
 AttemptID string `json:"attempt_id"`
 Operation string `json:"operation"`
 Target string `json:"target"`
 ArgumentsSHA256 string `json:"arguments_sha256"`
 Finding string `json:"finding"`
 VerifiedBy string `json:"verified_by"`
 InspectedAt string `json:"inspected_at"`
 Explanation string `json:"explanation"`
}

func readEvidence(path string)([]byte,error){
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path{
  return nil,errors.New("evidence requires a canonical absolute path")
 }
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return nil,errors.New("operator evidence file unavailable")}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{
  return nil,errors.New("operator evidence must be a private regular file")
 }
 owner,ok:=info.Sys().(*syscall.Stat_t)
 if !ok||owner.Uid!=0{
  return nil,errors.New("operator evidence must be root-owned")
 }
 raw,err:=io.ReadAll(io.LimitReader(file,maxEvidenceBytes+1))
 if err!=nil||len(raw)==0||int64(len(raw))>maxEvidenceBytes{
  return nil,errors.New("operator evidence could not be read within bound")
 }
 return raw,nil
}

func verifyEvidence(raw []byte,id,operation,target,digest string,started time.Time)(string,error){
 encoded,fields,err:=canonicaljson.Object(raw)
 if err!=nil||len(fields)!=8{
  return "",errors.New("reconciliation evidence must contain exactly eight fields")
 }
 for _,name:=range []string{"attempt_id","operation","target","arguments_sha256",
  "finding","verified_by","inspected_at","explanation"}{
  if _,ok:=fields[name];!ok{return "",errors.New("reconciliation evidence field missing")}
 }
 }
 var note evidence
 if err:=json.Unmarshal(encoded,&note);err!=nil{
  return "",errors.New("invalid reconciliation evidence")
 }
 if note.AttemptID!=id||note.Operation!=operation||note.Target!=target||
  note.ArgumentsSHA256!=digest{
  return "",errors.New("operator evidence does not match the unresolved attempt")
 }
 if note.Finding!="applied"&&note.Finding!="not_applied"{
  return "",errors.New("reconciliation requires a verified applied or not_applied finding")
 }
 if len(strings.TrimSpace(note.VerifiedBy))<2||len(note.VerifiedBy)>128||
  len(strings.TrimSpace(note.Explanation))<32||len(note.Explanation)>2000{
  return "",errors.New("operator identity and independent inspection explanation required")
 }
 inspected,err:=time.Parse(time.RFC3339,note.InspectedAt)
 if err!=nil||inspected.Before(started)||inspected.After(time.Now().UTC().Add(5*time.Minute)){
  return "",errors.New("reconciliation inspection timestamp is invalid")
 }
 checksum:=sha256.Sum256(raw)
 return hex.EncodeToString(checksum[:]),nil
}

func run(args []string)error{
 if os.Geteuid()!=0{
  return errors.New("audit recovery requires a trusted local root console")
 }
 if len(args)!=1 && (len(args)!=4||args[0]!="reconcile"||args[3]!="--confirm"){
  return errors.New("usage: homelab-audit-recovery unresolved | reconcile ID EVIDENCE_FILE --confirm")
 }
 state:=os.Getenv("HOMELAB_EXECUTOR_RELEASE_STATE")
 lease,err:=statelease.Acquire(state,"executord")
 if err!=nil{return fmt.Errorf("executor must be stopped before audit inspection: %w",err)}
 defer lease.Close()
 journal,err:=privileged.OpenAuditJournal(filepath.Join(state,"executor-actions.jsonl"))
 if err!=nil{return err}
 defer journal.Close()
 attempts,err:=journal.Unresolved()
 if err!=nil{return err}
 if len(args)==1{
  if args[0]!="unresolved"{return errors.New("unsupported audit inspection command")}
  payload,err:=json.Marshal(struct{
   Unresolved any `json:"unresolved"`
   Count int `json:"count"`
  }{Unresolved:attempts,Count:len(attempts)})
  if err!=nil{return err}
  fmt.Println(string(payload))
  return nil
 }
 id:=args[1]
 if len(id)!=32||strings.Trim(id,"0123456789abcdef")!=""{
  return errors.New("audit attempt ID must be exactly 32 lowercase hex characters")
 }
 for _,attempt:=range attempts{
  if attempt.ID!=id{continue}
  raw,err:=readEvidence(args[2])
  if err!=nil{return err}
  digest,err:=verifyEvidence(raw,attempt.ID,attempt.Operation,attempt.Target,
   attempt.ArgumentsSHA256,attempt.At)
  if err!=nil{return err}
  if err:=journal.Reconcile(id,digest);err!=nil{return err}
  fmt.Println("operator reconciliation recorded for audit identity "+id)
  return nil
 }
 return errors.New("audit identity is not an unresolved attempt")
}

func main(){
 if err:=run(os.Args[1:]);err!=nil{
  fmt.Fprintln(os.Stderr,err)
  os.Exit(1)
 }
}
