package health

import(
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"
)

type FoodProposal struct{
 ID string `json:"id"`
 Food string `json:"food"`
 Portion string `json:"portion"`
 EvidenceID string `json:"evidence_id,omitempty"`
 Origin string `json:"origin"`
 Nutrients map[string]float64 `json:"nutrients"`
 Units map[string]string `json:"units"`
 Notes string `json:"notes,omitempty"`
 Status string `json:"status"`
 ObservedAt time.Time `json:"observed_at"`
 CreatedAt time.Time `json:"created_at"`
 ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
}

func(s *Store)initFoodProposals()error{
 _,err:=s.db.Exec(`CREATE TABLE IF NOT EXISTS food_proposals(
  id TEXT PRIMARY KEY,document TEXT NOT NULL,status TEXT NOT NULL,
  created_at TEXT NOT NULL,confirmed_at TEXT)`)
 if err!=nil{return fmt.Errorf("food proposal database: %w",err)}
 return nil
}
func validateFoodProposal(p FoodProposal)error {
 if strings.TrimSpace(p.Food)==""||len(p.Food)>256||
  strings.TrimSpace(p.Portion)==""||len(p.Portion)>256{
  return errors.New("food name and portion description are required")
 }
 if p.Origin!="photo_estimate"&&p.Origin!="barcode_label"&&p.Origin!="recipe"&&p.Origin!="manual_estimate"{
  return errors.New("food origin must be photo_estimate, barcode_label, recipe or manual_estimate")
 }
 if p.ObservedAt.IsZero(){return errors.New("food observation timestamp is required")}
 if len(p.Notes)>4096||len(p.Nutrients)==0||len(p.Nutrients)>150{
  return errors.New("food notes or nutrient count outside allowed range")
 }
 if len(p.Units)!=len(p.Nutrients){return errors.New("each nutrient requires its own unit")}
 for k,v:=range p.Nutrients{
  if !identifier.MatchString(k)||!isFinite(v)||v<0||
    strings.TrimSpace(p.Units[k])==""||len(p.Units[k])>48{
   return errors.New("invalid nutrient metric, value or unit")
  }
 }
 for k:=range p.Units{if _,ok:=p.Nutrients[k];!ok{return errors.New("unit is provided for an absent nutrient")}}
 return nil
}
func(s *Store)SaveFoodProposal(ctx context.Context,p FoodProposal)(FoodProposal,error){
 if p.ID!=""||p.Status!=""{return FoodProposal{},errors.New("new food proposals must omit id and status")}
 if err:=validateFoodProposal(p);err!=nil{return FoodProposal{},err}
 if p.EvidenceID!=""{
  present,err:=s.EvidencePresent(ctx,p.EvidenceID)
  if err!=nil{return FoodProposal{},err}
  if !present{return FoodProposal{},errors.New("evidence_id does not exist")}
 }
 var err error
 p.ID,err=newID();if err!=nil{return FoodProposal{},err}
 p.Status="pending";p.ObservedAt=p.ObservedAt.UTC();p.CreatedAt=time.Now().UTC()
 raw,err:=json.Marshal(p);if err!=nil{return FoodProposal{},err}
 _,err=s.db.ExecContext(ctx,`INSERT INTO food_proposals(id,document,status,created_at)
 VALUES(?,?,?,?)`,p.ID,string(raw),p.Status,p.CreatedAt.Format(time.RFC3339Nano))
 if err!=nil{return FoodProposal{},err}
 return p,nil
}
func(s *Store)GetFoodProposal(ctx context.Context,id string)(FoodProposal,error){
 var raw,status string
 var confirmed sql.NullString
 err:=s.db.QueryRowContext(ctx,`SELECT document,status,confirmed_at FROM food_proposals WHERE id=?`,id).
 Scan(&raw,&status,&confirmed)
 if err!=nil{return FoodProposal{},err}
 var p FoodProposal
 if err=json.Unmarshal([]byte(raw),&p);err!=nil{return FoodProposal{},err}
 p.Status=status
 if confirmed.Valid {
  when,err:=time.Parse(time.RFC3339Nano,confirmed.String);if err!=nil{return FoodProposal{},err}
  p.ConfirmedAt=&when
 }
 return p,nil
}
func(s *Store)ListFoodProposals(ctx context.Context,status string,limit int)([]FoodProposal,error){
 if status!=""&&status!="pending"&&status!="confirmed"&&status!="discarded"{return nil,errors.New("invalid status")}
 if limit<1||limit>100{return nil,errors.New("limit must be 1 to 100")}
 q:="SELECT id FROM food_proposals";args:=[]any{}
 if status!=""{q+=" WHERE status=?";args=append(args,status)}
 q+=" ORDER BY created_at DESC,id DESC LIMIT ?";args=append(args,limit)
 rows,err:=s.db.QueryContext(ctx,q,args...);if err!=nil{return nil,err}
 ids:=[]string{}
 for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{break};ids=append(ids,id)}
 if err==nil{err=rows.Err()};_ =rows.Close();if err!=nil{return nil,err}
 proposals:=[]FoodProposal{}
 for _,id:=range ids{item,err:=s.GetFoodProposal(ctx,id);if err!=nil{return nil,err};proposals=append(proposals,item)}
 return proposals,nil
}
func(s *Store)DiscardFoodProposal(ctx context.Context,id string)error{
 result,err:=s.db.ExecContext(ctx,`UPDATE food_proposals SET status='discarded'
 WHERE id=? AND status='pending'`,id)
 if err!=nil{return err}
 n,err:=result.RowsAffected();if err!=nil{return err}
 if n!=1{return ErrConflict}
 return nil
}
func(s *Store)ConfirmFoodProposal(ctx context.Context,id string)(int,error){
 proposal,err:=s.GetFoodProposal(ctx,id);if err!=nil{return 0,err}
 if err=validateFoodProposal(proposal);err!=nil{return 0,err}
 meta,_:=json.Marshal(map[string]any{
  "food":proposal.Food,"portion":proposal.Portion,"proposal_id":proposal.ID,
  "origin":proposal.Origin,"evidence_id":proposal.EvidenceID,"notes":proposal.Notes,
  "uncertainty":"Nutrient amounts depend on the evidence and confirmed portion",
 })
 now:=time.Now().UTC()
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return 0,err}
 defer tx.Rollback()
 var status string
 err=tx.QueryRowContext(ctx,"SELECT status FROM food_proposals WHERE id=?",id).Scan(&status)
 if err!=nil{return 0,err}
 switch status{
 case "confirmed":return 0,nil
 case "pending":
 default:return 0,errors.New("food proposal is not pending")
 }
 imported:=0
 for metric,value:=range proposal.Nutrients{
  nutrientID,err:=newID();if err!=nil{return 0,err}
  method:="estimated"
  if proposal.Origin=="barcode_label"{method="reported"}
  result,err:=tx.ExecContext(ctx,`INSERT INTO records(
   id,domain,metric,value,unit,at,source,source_id,method,confidence,metadata,replaces,created_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(source,source_id) DO NOTHING`,
   nutrientID,"nutrition",metric,value,proposal.Units[metric],
   proposal.ObservedAt.UTC().Format(time.RFC3339Nano),"food_proposal",id+":"+metric,
   method,nil,string(meta),nil,now.Format(time.RFC3339Nano))
  if err!=nil{return 0,fmt.Errorf("confirm food nutrient: %w",err)}
  n,err:=result.RowsAffected();if err!=nil{return 0,err}
  if n==1{imported++}
 }
 changed,err:=tx.ExecContext(ctx,`UPDATE food_proposals
  SET status='confirmed',confirmed_at=? WHERE id=? AND status='pending'`,
  now.Format(time.RFC3339Nano),id)
 if err!=nil{return 0,err}
 count,err:=changed.RowsAffected()
 if err!=nil{return 0,err}
 if count!=1{return 0,ErrConflict}
 if err=tx.Commit();err!=nil{return 0,err}
 return imported,nil
}
