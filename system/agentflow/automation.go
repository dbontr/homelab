package agentflow

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "sort"
 "syscall"
 "time"
)

const maxAutomations = 100

// Automation stores an explicitly submitted read-only workflow program and a
// revisioned interval. It does not store model/provider secrets or credentials.
type Automation struct {
 ID string `json:"id"`
 Project string `json:"project"`
 Program json.RawMessage `json:"program"`
 EveryMinutes int `json:"every_minutes"`
 Enabled bool `json:"enabled"`
 Revision uint64 `json:"revision"`
 NextAt time.Time `json:"next_at"`
 LastAt time.Time `json:"last_at,omitempty"`
 LastWorkflowID string `json:"last_workflow_id,omitempty"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
}

func validateAutomation(a Automation)error{
 if !validID(a.ID)||!validID(a.Project)||len(a.Program)<2||
  len(a.Program)>32<<10||!json.Valid(a.Program)||
  a.EveryMinutes<60||a.EveryMinutes>10080 {
  return errors.New("invalid scheduled workflow")
 }
 return nil
}

func AutomationRunID(automationID string,slot time.Time)string{
 sum:=sha256.Sum256([]byte("eminence:automation:"+automationID+":"+slot.UTC().Format(time.RFC3339Nano)))
 return hex.EncodeToString(sum[:16])
}

func (s *Store) automationDir()(string,error){
 dir:=filepath.Join(s.dir,"automations")
 err:=os.Mkdir(dir,0700)
 if err!=nil&&!errors.Is(err,os.ErrExist){return "",err}
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0077!=0{
  return "",errors.New("automation state directory is not private")
 }
 return dir,nil
}

func (s *Store) automationPath(id string)(string,error){
 if !validID(id){return "",errors.New("invalid automation identity")}
 dir,err:=s.automationDir()
 if err!=nil{return "",err}
 return filepath.Join(dir,id+".json"),nil
}

func (s *Store) readAutomationLocked(id string)(Automation,error){
 var a Automation
 path,err:=s.automationPath(id)
 if err!=nil{return a,err}
 f,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW,0)
 if err!=nil{return a,err}
 defer f.Close()
 info,err:=f.Stat()
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||info.Size()>40<<10{
  return a,errors.New("unsafe automation record")
 }
 dec:=json.NewDecoder(io.LimitReader(f,40<<10))
 if err:=dec.Decode(&a);err!=nil{return a,err}
 var extra any
 if err:=dec.Decode(&extra);err!=io.EOF{return a,errors.New("automation record contains extra content")}
 if a.ID!=id{return a,errors.New("automation record ID mismatch")}
 if err:=validateAutomation(a);err!=nil{return a,err}
 return a,nil
}

func (s *Store) saveAutomationLocked(a Automation)error{
 if err:=validateAutomation(a);err!=nil{return err}
 path,err:=s.automationPath(a.ID)
 if err!=nil{return err}
 raw,err:=json.Marshal(a)
 if err!=nil{return err}
 if len(raw)>40<<10{return errors.New("automation record exceeds limit")}
 dir:=filepath.Dir(path)
 f,err:=os.CreateTemp(dir,".automation-")
 if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(raw);err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),path);err!=nil{return err}
 d,err:=os.Open(dir)
 if err!=nil{return err}
 defer d.Close()
 return d.Sync()
}

func (s *Store) listAutomationsLocked()([]Automation,error){
 dir,err:=s.automationDir()
 if err!=nil{return nil,err}
 files,err:=os.ReadDir(dir)
 if err!=nil{return nil,err}
 result:=make([]Automation,0,len(files))
 for _,file:=range files{
  if file.IsDir()||filepath.Ext(file.Name())!=".json"{continue}
  id:=file.Name()[:len(file.Name())-5]
  a,err:=s.readAutomationLocked(id)
  if err!=nil{return nil,err}
  result=append(result,a)
 }
 sort.Slice(result,func(i,j int)bool{return result[i].ID<result[j].ID})
 return result,nil
}

func (s *Store) CreateAutomation(a Automation)(Automation,error){
 if err:=validateAutomation(a);err!=nil{return Automation{},err}
 unlock,err:=s.lock()
 if err!=nil{return Automation{},err}
 defer unlock()
 rows,err:=s.listAutomationsLocked()
 if err!=nil{return Automation{},err}
 if len(rows)>=maxAutomations{return Automation{},errors.New("automation capacity reached")}
 for _,saved:=range rows{if saved.ID==a.ID{return Automation{},os.ErrExist}}
 a.CreatedAt=s.now().UTC()
 a.UpdatedAt=a.CreatedAt
 a.Revision=1
 a.LastAt=time.Time{}
 a.LastWorkflowID=""
 a.NextAt=a.CreatedAt.Add(time.Duration(a.EveryMinutes)*time.Minute)
 if err:=s.saveAutomationLocked(a);err!=nil{return Automation{},err}
 return a,nil
}

func (s *Store) ListAutomations()([]Automation,error){
 unlock,err:=s.lock()
 if err!=nil{return nil,err}
 defer unlock()
 return s.listAutomationsLocked()
}
func (s *Store) GetAutomation(id string)(Automation,error){
 unlock,err:=s.lock()
 if err!=nil{return Automation{},err}
 defer unlock()
 return s.readAutomationLocked(id)
}
func (s *Store) SetAutomationEnabled(id string,revision uint64,enabled bool)(Automation,error){
 unlock,err:=s.lock()
 if err!=nil{return Automation{},err}
 defer unlock()
 a,err:=s.readAutomationLocked(id)
 if err!=nil{return Automation{},err}
 if revision!=a.Revision{return Automation{},errors.New("automation revision conflict")}
 a.Enabled=enabled
 a.Revision++
 a.UpdatedAt=s.now().UTC()
 if enabled{a.NextAt=a.UpdatedAt.Add(time.Duration(a.EveryMinutes)*time.Minute)}
 if err:=s.saveAutomationLocked(a);err!=nil{return Automation{},err}
 return a,nil
}

func (s *Store) DueAutomations(now time.Time,limit int)([]Automation,error){
 if limit<1||limit>16{return nil,errors.New("invalid automation batch size")}
 unlock,err:=s.lock()
 if err!=nil{return nil,err}
 defer unlock()
 list,err:=s.listAutomationsLocked()
 if err!=nil{return nil,err}
 result:=make([]Automation,0,limit)
 for _,a:=range list{if a.Enabled&&!a.NextAt.After(now){result=append(result,a)}}
 sort.Slice(result,func(i,j int)bool{
  if !result[i].NextAt.Equal(result[j].NextAt){return result[i].NextAt.Before(result[j].NextAt)}
  return result[i].ID<result[j].ID
 })
 if len(result)>limit{result=result[:limit]}
 return result,nil
}

// FireAutomation holds a cross-process lock across idempotent workflow
// creation and schedule advancement. If the process fails between the two
// writes, the deterministic workflow ID recovers that exact occurrence.
func (s *Store) FireAutomation(a Automation,w Workflow)(Workflow,error){
 if err:=Validate(w);err!=nil{return Workflow{},err}
 if w.AutomationID!=a.ID||!w.AutomationDue.Equal(a.NextAt)||
  w.Project!=a.Project||w.ID!=AutomationRunID(a.ID,a.NextAt)||
  !w.CapabilityPlan||w.PersonalWrite||w.AutoReplan {
  return Workflow{},errors.New("automation workflow identity or scope mismatch")
 }
 unlock,err:=s.lock()
 if err!=nil{return Workflow{},err}
 defer unlock()
 current,err:=s.readAutomationLocked(a.ID)
 if err!=nil{return Workflow{},err}
 now:=s.now().UTC()
 if current.Revision!=a.Revision||current.Project!=a.Project||
  !current.Enabled||current.NextAt.After(now)||!current.NextAt.Equal(a.NextAt)||
  !bytes.Equal(current.Program,a.Program) {
  return Workflow{},errors.New("automation scheduling revision conflict")
 }
 persisted,err:=s.read(w.ID)
 if errors.Is(err,os.ErrNotExist){
  persisted,err=s.readArchive(w.ID)
  if errors.Is(err,os.ErrNotExist){
   if err:=s.createLocked(w);err!=nil{return Workflow{},err}
   persisted,err=s.read(w.ID)
  }
 }
 if err!=nil{return Workflow{},err}
 if persisted.AutomationID!=a.ID||!persisted.AutomationDue.Equal(a.NextAt)||
  persisted.Project!=a.Project{
  return Workflow{},errors.New("automation run identity collision")
 }
 current.Revision++
 current.LastAt=a.NextAt
 current.LastWorkflowID=w.ID
 current.NextAt=now.Add(time.Duration(current.EveryMinutes)*time.Minute)
 current.UpdatedAt=now
 if err:=s.saveAutomationLocked(current);err!=nil{return Workflow{},err}
 return persisted,nil
}
