// Package recipes stores first-party recipes and their planned meals.
package recipes

import (
	"crypto/rand"
    "crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("recipe or meal not found")
	ErrConflict = errors.New("record revision conflict")
)

type Ingredient struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit,omitempty"`
	Notes  string  `json:"notes,omitempty"`
}

type Step struct {
	Text string `json:"text"`
}

type Nutrient struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

type Nutrition struct {
	Method     string     `json:"method"`
	Source     string     `json:"source,omitempty"`
	PerServing []Nutrient `json:"per_serving"`
}

type Recipe struct {
	ID          string      `json:"id,omitempty"`
	Title       string      `json:"title"`
	Summary     string      `json:"summary,omitempty"`
	Servings    float64     `json:"servings"`
	PrepMinutes int         `json:"prep_minutes,omitempty"`
	CookMinutes int         `json:"cook_minutes,omitempty"`
	Ingredients []Ingredient `json:"ingredients"`
	Steps       []Step      `json:"steps"`
	Tags        []string    `json:"tags,omitempty"`
	Equipment   []string    `json:"equipment,omitempty"`
	SourceURL   string      `json:"source_url,omitempty"`
	ImportRef   string      `json:"import_ref,omitempty"`
	ImageRef    string      `json:"image_ref,omitempty"`
	Notes       string      `json:"notes,omitempty"`
	LastMade    string      `json:"last_made,omitempty"`
	Nutrition   *Nutrition  `json:"nutrition,omitempty"`
	Revision    int         `json:"revision"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type Meal struct {
	ID        string    `json:"id,omitempty"`
	RecipeID  string    `json:"recipe_id"`
	Date      string    `json:"date"`
	MealType  string    `json:"meal_type,omitempty"`
	Servings  float64   `json:"servings"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type GroceryItem struct {
    Key       string   `json:"key"`
    Checked   bool     `json:"checked"`
	Name      string   `json:"name"`
	Amount    float64  `json:"amount"`
	Unit      string   `json:"unit,omitempty"`
	RecipeIDs []string `json:"recipe_ids"`
}

type GroceryBoard struct {
    Revision int `json:"revision"`
    Items []GroceryItem `json:"items"`
}

type NutritionPreview struct {
	RecipeID string     `json:"recipe_id"`
	Servings float64    `json:"servings"`
	Method   string     `json:"method"`
	Source   string     `json:"source,omitempty"`
	Nutrients []Nutrient `json:"nutrients"`
}

type Document struct {
	Version int      `json:"version"`
    GroceryChecks map[string]bool `json:"grocery_checks,omitempty"`
	Recipes []Recipe `json:"recipes"`
	Meals   []Meal   `json:"meals"`
	History map[string][]Recipe `json:"history,omitempty"`
}

type snapshot struct {
    GroceryRevision int `json:"grocery_revision,omitempty"`
    GroceryChecks map[string]bool `json:"grocery_checks,omitempty"`
	Version  int               `json:"version"`
	Recipes  map[string]Recipe `json:"recipes"`
	Meals    map[string]Meal   `json:"meals"`
	History  map[string][]Recipe `json:"history,omitempty"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data snapshot
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("recipe database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil { return nil, err }
	if !dirInfo.IsDir() || dirInfo.Mode().Perm()&0077 != 0 {
		return nil, errors.New("recipe database directory must be private")
	}
	if fileInfo, err := os.Lstat(path); err == nil {
		if !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm()&0077 != 0 {
			return nil, errors.New("recipe database must be a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s := &Store{path: path, data: snapshot{Version: 1, Recipes: map[string]Recipe{}, Meals: map[string]Meal{}, History: map[string][]Recipe{}}}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &s.data); err != nil {
		return nil, fmt.Errorf("decode recipes: %w", err)
	}
	if s.data.Version != 1 {
		return nil, errors.New("unsupported recipe database version")
	}
	if s.data.Recipes == nil { s.data.Recipes = map[string]Recipe{} }
	if s.data.Meals == nil { s.data.Meals = map[string]Meal{} }
 if s.data.GroceryChecks == nil { s.data.GroceryChecks = map[string]bool{} }
	if s.data.History == nil { s.data.History = map[string][]Recipe{} }
	for id, recipe := range s.data.Recipes {
		if id != recipe.ID || !validID(id) { return nil, errors.New("recipe identifier mismatch") }
		if err := validateRecipe(recipe); err != nil { return nil, fmt.Errorf("stored recipe %s: %w", id, err) }
	}
	for id, versions := range s.data.History {
		current, ok := s.data.Recipes[id]
		if !ok { return nil, errors.New("recipe history references missing recipe") }
		if err := validateHistory(id, current, versions); err != nil {
			return nil, fmt.Errorf("stored recipe %s: %w", id, err)
		}
	}
	seenImports := make(map[string]string)
	for id, recipe := range s.data.Recipes {
		if recipe.ImportRef == "" { continue }
		if old, exists := seenImports[recipe.ImportRef]; exists {
			return nil, fmt.Errorf("duplicate recipe import reference for %s and %s", old, id)
		}
		seenImports[recipe.ImportRef] = id
	}
	for id, meal := range s.data.Meals {
		if id != meal.ID || !validID(id) { return nil, errors.New("meal identifier mismatch") }
		if err := validateMeal(meal); err != nil { return nil, err }
		if _, ok := s.data.Recipes[meal.RecipeID]; !ok {
			return nil, errors.New("meal references missing recipe")
		}
	}
	return s, nil
}

func duplicate(s snapshot) snapshot {
	out := snapshot{Version: 1, Recipes: make(map[string]Recipe, len(s.Recipes)), Meals: make(map[string]Meal, len(s.Meals)), GroceryRevision:s.GroceryRevision, GroceryChecks:make(map[string]bool,len(s.GroceryChecks))}
 for key,checked:=range s.GroceryChecks{out.GroceryChecks[key]=checked}
	for key, value := range s.Recipes { out.Recipes[key] = value }
	for key, value := range s.Meals { out.Meals[key] = value }
	out.History = make(map[string][]Recipe, len(s.History))
	for id, versions := range s.History { out.History[id] = copyHistory(versions) }
	return out
}

func (s *Store) save(next snapshot) error {
	body, err := json.MarshalIndent(next, "", "  ")
	if err != nil { return err }
	file, err := os.CreateTemp(filepath.Dir(s.path), ".recipes-*")
	if err != nil { return err }
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil { file.Close(); return err }
	if _, err := file.Write(body); err != nil { file.Close(); return err }
	if err := file.Sync(); err != nil { file.Close(); return err }
	if err := file.Close(); err != nil { return err }
	if err := os.Rename(file.Name(), s.path); err != nil { return err }
	if dir, err := os.Open(filepath.Dir(s.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	s.data = next
	return nil
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil { return "", err }
	return hex.EncodeToString(bytes[:]), nil
}

func validID(value string) bool {
	if len(value) != 32 { return false }
	for _, digit := range value {
		if !(digit >= '0' && digit <= '9') && !(digit >= 'a' && digit <= 'f') {
			return false
		}
	}
	return true
}

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 1000000
}

func validDate(value string) bool {
	if value == "" { return true }
	d, err := time.Parse("2006-01-02", value)
	return err == nil && d.Format("2006-01-02") == value
}

func validateRecipe(r Recipe) error {
	if strings.TrimSpace(r.Title) == "" || len(r.Title) > 200 || len(r.Summary) > 3000 || len(r.Notes) > 10000 {
		return errors.New("invalid recipe title, summary or notes")
	}
	if !finitePositive(r.Servings) || r.Servings > 1000 || r.PrepMinutes < 0 || r.CookMinutes < 0 || r.PrepMinutes > 10080 || r.CookMinutes > 10080 {
		return errors.New("invalid yield or preparation time")
	}
	if len(r.Ingredients) == 0 || len(r.Ingredients) > 250 || len(r.Steps) == 0 || len(r.Steps) > 200 {
		return errors.New("recipes require ingredients and method steps")
	}
	for _, item := range r.Ingredients {
		if strings.TrimSpace(item.Name) == "" || len(item.Name) > 200 ||
			!finitePositive(item.Amount) || len(item.Unit) > 48 || len(item.Notes) > 1000 {
			return errors.New("invalid ingredient name, amount, unit or notes")
		}
	}
	for _, step := range r.Steps {
		if strings.TrimSpace(step.Text) == "" || len(step.Text) > 4000 {
			return errors.New("invalid recipe step")
		}
	}
	if len(r.Tags) > 40 || len(r.Equipment) > 40 { return errors.New("too many tags or equipment entries") }
	for _, text := range append(append([]string{}, r.Tags...), r.Equipment...) {
		if strings.TrimSpace(text) == "" || len(text) > 100 { return errors.New("invalid tag or equipment") }
	}
	if len(r.SourceURL) > 2000 || len(r.ImageRef) > 2000 || !validDate(r.LastMade) { return errors.New("invalid source, image or last-made date") }
	if r.ImportRef != "" && !validRecipeSageRef(r.ImportRef) { return errors.New("invalid recipe import reference") }
	if r.ID != "" && !validID(r.ID) { return errors.New("invalid recipe ID") }
	if r.SourceURL != "" {
		u, err := url.Parse(r.SourceURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return errors.New("source URL must use HTTPS")
		}
	}
	if r.ImageRef != "" && !validImageRef(r.ImageRef) {
		return errors.New("image reference must identify a stored recipe image")
	}
	if n := r.Nutrition; n != nil {
		if n.Method != "measured" && n.Method != "estimated" && n.Method != "reported" {
			return errors.New("nutrition provenance is required")
		}
		if len(n.Source) > 500 || len(n.PerServing) > 150 { return errors.New("invalid nutrition data") }
		for _, nutrient := range n.PerServing {
			if strings.TrimSpace(nutrient.Name) == "" || len(nutrient.Name) > 80 ||
				len(nutrient.Unit) == 0 || len(nutrient.Unit) > 32 ||
				math.IsNaN(nutrient.Amount) || math.IsInf(nutrient.Amount, 0) || nutrient.Amount < 0 {
				return errors.New("invalid nutrient value")
			}
		}
	}
	return nil
}

func validateMeal(m Meal) error {
	if !validID(m.RecipeID) || (m.ID != "" && !validID(m.ID)) || !validDate(m.Date) || m.Date == "" ||
		!finitePositive(m.Servings) || m.Servings > 1000 || len(m.MealType) > 40 || len(m.Notes) > 2000 {
		return errors.New("invalid scheduled meal")
	}
	return nil
}

func copyRecipe(r Recipe) Recipe {
	r.Ingredients = append([]Ingredient(nil), r.Ingredients...)
	r.Steps = append([]Step(nil), r.Steps...)
	r.Tags = append([]string(nil), r.Tags...)
	r.Equipment = append([]string(nil), r.Equipment...)
	if r.Nutrition != nil {
		n := *r.Nutrition
		n.PerServing = append([]Nutrient(nil), n.PerServing...)
		r.Nutrition = &n
	}
	return r
}

func (s *Store) List(query string) []Recipe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(strings.TrimSpace(query))
	list := make([]Recipe, 0, len(s.data.Recipes))
	for _, recipe := range s.data.Recipes {
		if q != "" && !strings.Contains(strings.ToLower(recipe.Title+" "+recipe.Summary+" "+strings.Join(recipe.Tags, " ")), q) { continue }
		list = append(list, copyRecipe(recipe))
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Title) < strings.ToLower(list[j].Title) })
	return list
}

func (s *Store) Get(id string) (Recipe, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recipe, ok := s.data.Recipes[id]
	if !ok { return Recipe{}, ErrNotFound }
	return copyRecipe(recipe), nil
}

// Save uses revision zero to create and the stored revision to edit.
func (s *Store) Save(recipe Recipe) (Recipe, error) {
	recipe.Title = strings.TrimSpace(recipe.Title)
	if err := validateRecipe(recipe); err != nil { return Recipe{}, err }
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	next := duplicate(s.data)
	if recipe.ID == "" {
		if recipe.ImportRef != "" { return Recipe{}, errors.New("source provenance is assigned by the import operation") }
		if recipe.Revision != 0 { return Recipe{}, ErrConflict }
		id, err := randomID()
		if err != nil { return Recipe{}, err }
		recipe.ID, recipe.Revision, recipe.CreatedAt = id, 1, now
	} else {
		old, ok := s.data.Recipes[recipe.ID]
		if !ok { return Recipe{}, ErrNotFound }
		if recipe.Revision != old.Revision { return Recipe{}, ErrConflict }
		if recipe.ImportRef != old.ImportRef { return Recipe{}, ErrConflict }
		next.History[recipe.ID] = appendRevision(next.History[recipe.ID], old)
		recipe.Revision = old.Revision + 1
		recipe.CreatedAt = old.CreatedAt
	}
	recipe.UpdatedAt = now
	next.Recipes[recipe.ID] = copyRecipe(recipe)
	if err := s.save(next); err != nil { return Recipe{}, err }
	return copyRecipe(recipe), nil
}

func (s *Store) Delete(id string, expected int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recipe, ok := s.data.Recipes[id]
	if !ok { return ErrNotFound }
	if expected != recipe.Revision { return ErrConflict }
	for _, meal := range s.data.Meals {
		if meal.RecipeID == id { return ErrConflict }
	}
	next := duplicate(s.data)
	delete(next.Recipes, id)
	delete(next.History, id)
	return s.save(next)
}

func (s *Store) Meals() []Meal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Meal, 0, len(s.data.Meals))
	for _, meal := range s.data.Meals { out = append(out, meal) }
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date == out[j].Date { return out[i].ID < out[j].ID }
		return out[i].Date < out[j].Date
	})
	return out
}

func (s *Store) AddMeal(meal Meal) (Meal, error) {
	if meal.ID != "" { return Meal{}, errors.New("meal ID is assigned by the server") }
	if err := validateMeal(meal); err != nil { return Meal{}, err }
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.Recipes[meal.RecipeID]; !ok { return Meal{}, ErrNotFound }
	id, err := randomID()
	if err != nil { return Meal{}, err }
	meal.ID, meal.CreatedAt = id, time.Now().UTC()
	next := duplicate(s.data)
	next.Meals[id] = meal
	if err := s.save(next); err != nil { return Meal{}, err }
	return meal, nil
}

func (s *Store) DeleteMeal(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.Meals[id]; !ok { return ErrNotFound }
	next := duplicate(s.data)
	delete(next.Meals, id)
	return s.save(next)
}

// Groceries is a derived view. Ingredients with different units remain distinct;
// no mass-to-volume conversion is attempted without a measured density.
func (s *Store) Groceries(start,end string)([]GroceryItem,error){
 if !validDate(start)||!validDate(end)||(start!=""&&end!=""&&start>end){return nil,errors.New("invalid grocery date range")}
 s.mu.RLock();defer s.mu.RUnlock()
 return collectGroceries(s.data,start,end),nil
}

func groceryKey(name,unit string) string {
 value:=strings.ToLower(strings.TrimSpace(name))+"\x00"+strings.ToLower(strings.TrimSpace(unit))
 sum:=sha256.Sum256([]byte(value))
 return hex.EncodeToString(sum[:])
}

func collectGroceries(data snapshot,start,end string)[]GroceryItem{
 items:=map[string]*GroceryItem{}
 for _,meal:=range data.Meals{
  if (start!=""&&meal.Date<start)||(end!=""&&meal.Date>end){continue}
  recipe:=data.Recipes[meal.RecipeID]
  scale:=meal.Servings/recipe.Servings
  for _,ingredient:=range recipe.Ingredients{
   key:=groceryKey(ingredient.Name,ingredient.Unit)
   item,found:=items[key]
   if !found{
    item=&GroceryItem{Key:key,Name:ingredient.Name,Unit:ingredient.Unit,Checked:data.GroceryChecks[key],RecipeIDs:[]string{}}
    items[key]=item
   }
   item.Amount+=ingredient.Amount*scale
   source:=false
   for _,id:=range item.RecipeIDs{if id==recipe.ID{source=true;break}}
   if !source{item.RecipeIDs=append(item.RecipeIDs,recipe.ID)}
  }
 }
 result:=make([]GroceryItem,0,len(items))
 for _,item:=range items{sort.Strings(item.RecipeIDs);result=append(result,*item)}
 sort.Slice(result,func(i,j int)bool{
  if strings.EqualFold(result[i].Name,result[j].Name){return result[i].Unit<result[j].Unit}
  return strings.ToLower(result[i].Name)<strings.ToLower(result[j].Name)
 })
 return result
}

func (s *Store) Board(start,end string)(GroceryBoard,error){
 if !validDate(start)||!validDate(end)||(start!=""&&end!=""&&start>end){return GroceryBoard{},errors.New("invalid grocery date range")}
 s.mu.RLock();defer s.mu.RUnlock()
 return GroceryBoard{Revision:s.data.GroceryRevision,Items:collectGroceries(s.data,start,end)},nil
}

func (s *Store) MarkGrocery(key string,checked bool,expected int)(GroceryBoard,error){
 if len(key)!=64{return GroceryBoard{},errors.New("invalid grocery key")}
 for _,c:=range key{if !strings.ContainsRune("0123456789abcdef",c){return GroceryBoard{},errors.New("invalid grocery key")}}
 s.mu.Lock();defer s.mu.Unlock()
 if expected!=s.data.GroceryRevision{return GroceryBoard{},ErrConflict}
 found:=false
 for _,item:=range collectGroceries(s.data,"",""){if item.Key==key{found=true;break}}
 if !found{return GroceryBoard{},ErrNotFound}
 next:=duplicate(s.data)
 if checked{next.GroceryChecks[key]=true}else{delete(next.GroceryChecks,key)}
 next.GroceryRevision++
 if err:=s.save(next);err!=nil{return GroceryBoard{},err}
 return GroceryBoard{Revision:next.GroceryRevision,Items:collectGroceries(next,"","")},nil
}

func (s *Store) Nutrition(id string, servings float64) (NutritionPreview, error) {
	if !finitePositive(servings) || servings > 1000 { return NutritionPreview{}, errors.New("invalid portion") }
	recipe, err := s.Get(id)
	if err != nil { return NutritionPreview{}, err }
	if recipe.Nutrition == nil { return NutritionPreview{}, ErrNotFound }
	result := NutritionPreview{RecipeID: recipe.ID, Servings: servings, Method: recipe.Nutrition.Method,
		Source: recipe.Nutrition.Source, Nutrients: make([]Nutrient, len(recipe.Nutrition.PerServing))}
	for i, item := range recipe.Nutrition.PerServing {
		item.Amount *= servings
		result.Nutrients[i] = item
	}
	return result, nil
}

// Export is a portable, versioned JSON document; importing does not overwrite existing records.
func (s *Store) Export() Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Document{Version: 1, GroceryChecks:make(map[string]bool,len(s.data.GroceryChecks)), Recipes: make([]Recipe, 0, len(s.data.Recipes)), Meals: make([]Meal, 0, len(s.data.Meals)), History:make(map[string][]Recipe,len(s.data.History))}
	for key,checked:=range s.data.GroceryChecks{out.GroceryChecks[key]=checked}
 for _, recipe := range s.data.Recipes { out.Recipes = append(out.Recipes, copyRecipe(recipe)) }
	for _, meal := range s.data.Meals { out.Meals = append(out.Meals, meal) }
	for id, versions := range s.data.History { out.History[id] = copyHistory(versions) }
	sort.Slice(out.Recipes, func(i,j int) bool { return out.Recipes[i].ID < out.Recipes[j].ID })
	sort.Slice(out.Meals, func(i,j int) bool { return out.Meals[i].ID < out.Meals[j].ID })
	return out
}

func (s *Store) Import(doc Document) (int, error) {
	if doc.Version != 1 || len(doc.Recipes) > 5000 || len(doc.Meals) > 10000 {
		return 0, errors.New("unsupported or oversized recipe archive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := duplicate(s.data)
	imported := make(map[string]Recipe, len(doc.Recipes))
	importRefs := make(map[string]bool, len(s.data.Recipes))
	for _, existing := range s.data.Recipes {
		if existing.ImportRef != "" { importRefs[existing.ImportRef] = true }
	}
	for _, recipe := range doc.Recipes {
		if !validID(recipe.ID) || recipe.Revision < 1 || recipe.CreatedAt.IsZero() || recipe.UpdatedAt.IsZero() { return 0, errors.New("import requires valid IDs, revisions and timestamps") }
		if _, exists := next.Recipes[recipe.ID]; exists { return 0, ErrConflict }
		if err := validateRecipe(recipe); err != nil { return 0, err }
		if recipe.ImportRef != "" {
			if importRefs[recipe.ImportRef] { return 0, ErrConflict }
			importRefs[recipe.ImportRef] = true
		}
		next.Recipes[recipe.ID] = copyRecipe(recipe)
		imported[recipe.ID] = recipe
	}
	for id, versions := range doc.History {
		current, exists := imported[id]
		if !exists { return 0, errors.New("recipe history requires a matching imported recipe") }
		if err := validateHistory(id, current, versions); err != nil { return 0, err }
		next.History[id] = copyHistory(versions)
	}
	for _, meal := range doc.Meals {
		if !validID(meal.ID) || meal.CreatedAt.IsZero() { return 0, errors.New("imported meal ID and creation timestamp are required") }
		if _, exists := next.Meals[meal.ID]; exists { return 0, ErrConflict }
		if err := validateMeal(meal); err != nil { return 0, err }
		if _, exists := next.Recipes[meal.RecipeID]; !exists { return 0, ErrNotFound }
		next.Meals[meal.ID] = meal
	}
	if len(doc.GroceryChecks)>0 {
   if len(s.data.Recipes)>0||len(s.data.Meals)>0{return 0,ErrConflict}
   available:=map[string]bool{}
   for _,item:=range collectGroceries(next,"",""){available[item.Key]=true}
   for key,checked:=range doc.GroceryChecks{
     if !available[key]{return 0,errors.New("grocery archive contains unknown items")}
     if checked{next.GroceryChecks[key]=true}
   }
 }
 if err := s.save(next); err != nil { return 0, err }
 return len(doc.Recipes), nil
}
