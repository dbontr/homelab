package health

import(
 "context"
 "database/sql"
 "errors"
 "time"
)
type SyncCursor struct{
 Name string `json:"name"`
 Filter string `json:"filter"`
 PageToken string `json:"page_token"`
 UpdatedAt time.Time `json:"updated_at"`
}
func(s *Store)initSyncCursors()error{
 _,err:=s.db.Exec(`CREATE TABLE IF NOT EXISTS sync_cursors(
  name TEXT PRIMARY KEY,filter TEXT NOT NULL,page_token TEXT NOT NULL,
  updated_at TEXT NOT NULL)`)
 return err
}
func(s *Store)GetSyncCursor(ctx context.Context,name string)(SyncCursor,error){
 var c SyncCursor;var updated string
 err:=s.db.QueryRowContext(ctx,`SELECT name,filter,page_token,updated_at FROM sync_cursors
 WHERE name=?`,name).Scan(&c.Name,&c.Filter,&c.PageToken,&updated)
 if err!=nil{return SyncCursor{},err}
 c.UpdatedAt,err=time.Parse(time.RFC3339Nano,updated)
 return c,err
}
func(s *Store)PutSyncCursor(ctx context.Context,name,filter,pageToken string)error{
 if name==""||len(name)>128||len(filter)>1024||len(pageToken)>8192{
  return errors.New("invalid synchronization cursor")
 }
 _,err:=s.db.ExecContext(ctx,`INSERT INTO sync_cursors(name,filter,page_token,updated_at)
 VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET
 filter=excluded.filter,page_token=excluded.page_token,updated_at=excluded.updated_at`,
 name,filter,pageToken,time.Now().UTC().Format(time.RFC3339Nano))
 return err
}
func(s *Store)ClearSyncCursor(ctx context.Context,name string)error{
 if name==""{return errors.New("cursor name is required")}
 _,err:=s.db.ExecContext(ctx,"DELETE FROM sync_cursors WHERE name=?",name)
 return err
}
func IsSyncCursorMissing(err error)bool{return errors.Is(err,sql.ErrNoRows)}
