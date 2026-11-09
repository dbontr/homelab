package health

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "sort"
 "strings"
 "time"
)

type MealItem struct {
 ID string `json:"id,omitempty"`
 Name string `json:"name"`
 Quantity float64 `json:"quantity"`
 Unit string `json:"unit"`
 RecipeID string `json:"recipe_id,omitempty"`
 Notes string `json:"notes,omitempty"`
}
type GroceryItem struct {
 ID string `json:"id"`
 Name string `json:"name"`
 Quantity float64 `json:"quantity"`
 Unit string `json:"unit"`
 Checked bool `json:"checked"`
 RecipeID string `json:"recipe_id,omitempty"`
}
type GroceryList struct {
 ID string `json:"id"`
 Title string `json:"title"`
 Items []GroceryItem `json:"items"`
 Revision int `json:"revision"`
 UpdatedAt time.Time `json:"updated_at"`
}

func(s *Store)initGroceries()error {
 for _,query:=range []string{
  `CREATE TABLE IF NOT EXISTS grocery_lists(
  id TEXT PRIMARY KEY,title TEXT NOT NULL,items TEXT NOT NULL,
  revision INTEGER NOT NULL,updated_at TEXT NOT NULL)`,
  `CREATE TABLE IF NOT EXISTS grocery_history(
  list_id TEXT NOT NULL REFERENCES grocery_lists(id),revision INTEGER NOT NULL,
  document TEXT NOT NULL,changed_at TEXT NOT NULL,PRIMARY KEY(list_id,revision))`,
 }{
  if _,err:=s.db.Exec(query);err!=nil{return fmt.Errorf("grocery schema: %w",err)}
 }
 return nil
}
func validateMealItem(item MealItem)error{
 if strings.TrimSpace(item.Name)==""||len(item.Name)>200{return errors.New("meal item name is required")}
 if !isFinite(item.Quantity)||item.Quantity<=0||item.Quantity>1000000{
  return errors.New("meal item quantity must be positive and finite")
 }
 if item.Unit==""||len(item.Unit)>48||len(item.RecipeID)>128||len(item.Notes)>1000{
  return errors.New("meal item units, recipe ID or notes are invalid")
 }
 return nil
}
func validateGrocery(g GroceryList)error {
 if strings.TrimSpace(g.Title)==""||len(g.Title)>200{return errors.New("grocery list title is required")}
 if len(g.Items)>2000{return errors.New("grocery list exceeds 2000 items")}
 seen:=map[string]bool{}
 for _,item:=range g.Items{
  if item.ID==""||len(item.ID)>128||seen[item.ID]{
   return errors.New("grocery item IDs must be unique and nonempty")
  }
  seen[item.ID]=true
  if err:=validateMealItem(MealItem{Name:item.Name,Quantity:item.Quantity,Unit:item.Unit,RecipeID:item.RecipeID});err!=nil{return err}
 }
 return nil
}
func(s *Store) GroceryFromPlan(ctx context.Context,planID string)([]GroceryItem,error){
 p,err:=s.GetPlan(ctx,planID);if err!=nil{return nil,err}
 if p.Kind!="meal"{return nil,errors.New("grocery preview requires a meal plan")}
 grouped:=map[string]GroceryItem{}
 for _,session:=range p.Sessions{
  for _,meal:=range session.Items{
   if err:=validateMealItem(meal);err!=nil{return nil,err}
   key:=strings.ToLower(strings.TrimSpace(meal.Name))+"\x00"+meal.Unit+"\x00"+meal.RecipeID
   item,ok:=grouped[key]
   if !ok{
    item.ID,err=newID();if err!=nil{return nil,err}
    item.Name=meal.Name;item.Unit=meal.Unit;item.RecipeID=meal.RecipeID
   }
   item.Quantity+=meal.Quantity;grouped[key]=item
  }
 }
 items:=make([]GroceryItem,0,len(grouped))
 for _,item:=range grouped{items=append(items,item)}
 sort.Slice(items,func(i,j int)bool{return items[i].Name<items[j].Name})
 return items,nil
}
func(s *Store) SaveGrocery(ctx context.Context,g GroceryList,expected int)(GroceryList,error){
 if expected<0{return GroceryList{},errors.New("expected revision must be nonnegative")}
 if err:=validateGrocery(g);err!=nil{return GroceryList{},err}
 now:=time.Now().UTC()
 g.UpdatedAt=now
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return GroceryList{},err}
 defer tx.Rollback()
 body,err:=json.Marshal(g.Items);if err!=nil{return GroceryList{},err}
 if expected==0{
  if g.ID!=""{return GroceryList{},errors.New("new grocery lists must omit id")}
  g.ID,err=newID();if err!=nil{return GroceryList{},err}
  g.Revision=1
  _,err=tx.ExecContext(ctx,`INSERT INTO grocery_lists(id,title,items,revision,updated_at)
    VALUES(?,?,?,?,?)`,g.ID,g.Title,string(body),g.Revision,now.Format(time.RFC3339Nano))
 }else{
  if g.ID==""{return GroceryList{},errors.New("grocery list id is required for edits")}
  g.Revision=expected+1
  var result sql.Result
  result,err=tx.ExecContext(ctx,`UPDATE grocery_lists SET title=?,items=?,revision=?,updated_at=?
   WHERE id=? AND revision=?`,g.Title,string(body),g.Revision,now.Format(time.RFC3339Nano),g.ID,expected)
  if err==nil{
   var n int64;n,err=result.RowsAffected()
   if err==nil&&n==0{return GroceryList{},ErrConflict}
  }
 }
 if err!=nil{return GroceryList{},err}
 snap,err:=json.Marshal(g);if err!=nil{return GroceryList{},err}
 _,err=tx.ExecContext(ctx,`INSERT INTO grocery_history(list_id,revision,document,changed_at)
 VALUES(?,?,?,?)`,g.ID,g.Revision,string(snap),now.Format(time.RFC3339Nano))
 if err!=nil{return GroceryList{},err}
 if err=tx.Commit();err!=nil{return GroceryList{},err}
 return g,nil
}
func(s *Store) GetGrocery(ctx context.Context,id string)(GroceryList,error){
 var g GroceryList;var raw,updated string
 err:=s.db.QueryRowContext(ctx,`SELECT id,title,items,revision,updated_at FROM grocery_lists WHERE id=?`,id).
 Scan(&g.ID,&g.Title,&raw,&g.Revision,&updated)
 if err!=nil{return GroceryList{},err}
 if err=json.Unmarshal([]byte(raw),&g.Items);err!=nil{return GroceryList{},err}
 g.UpdatedAt,err=time.Parse(time.RFC3339Nano,updated)
 return g,err
}
func(s *Store) ListGroceries(ctx context.Context)([]GroceryList,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT id FROM grocery_lists ORDER BY updated_at DESC LIMIT 100`)
 if err!=nil{return nil,err}
 ids:=[]string{}
 for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{break};ids=append(ids,id)}
 if err==nil{err=rows.Err()};_ =rows.Close();if err!=nil{return nil,err}
 lists:=[]GroceryList{}
 for _,id:=range ids{g,e:=s.GetGrocery(ctx,id);if e!=nil{return nil,e};lists=append(lists,g)}
 return lists,nil
}
