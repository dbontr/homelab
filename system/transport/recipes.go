package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"io"
	"mime"
	"strconv"
	"strings"

	"github.com/dbontr/homelab-new/system/recipes"
)

func (s *Server) SetRecipes(db *recipes.Store) { s.recipes = db }

func recipeFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recipes.ErrNotFound):
		writeError(w, http.StatusNotFound, "recipe or meal not found")
	case errors.Is(err, recipes.ErrConflict):
		writeError(w, http.StatusConflict, "record revision conflicts or recipe is scheduled")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) recipeCollection(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil { writeError(w, 503, "recipe storage is not configured"); return }
	if r.Method == http.MethodGet {
		q := r.URL.Query().Get("query")
		if len(q) > 200 { writeError(w, 400, "query too long"); return }
		writeJSON(w, 200, s.recipes.List(q))
		return
	}
	var recipe recipes.Recipe
	if err := decode(r, &recipe); err != nil { writeError(w, 400, err.Error()); return }
	if recipe.ID != "" || recipe.Revision != 0 { writeError(w, 400, "new recipes cannot supply IDs or revisions"); return }
	saved, err := s.recipes.Save(recipe)
	if err != nil { recipeFailure(w, err); return }
	w.Header().Set("ETag", "\""+strconv.Itoa(saved.Revision)+"\"")
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) recipeItem(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil { writeError(w, 503, "recipe storage is not configured"); return }
	id := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		result, err := s.recipes.Get(id)
		if err != nil { recipeFailure(w, err); return }
		w.Header().Set("ETag", "\""+strconv.Itoa(result.Revision)+"\"")
		writeJSON(w, 200, result)
	case http.MethodPut:
		var item recipes.Recipe
		if err := decode(r, &item); err != nil { writeError(w, 400, err.Error()); return }
		if item.ID != id { writeError(w, 400, "recipe ID does not match the route"); return }
		result, err := s.recipes.Save(item)
		if err != nil { recipeFailure(w, err); return }
		w.Header().Set("ETag", "\""+strconv.Itoa(result.Revision)+"\"")
		writeJSON(w, 200, result)
	case http.MethodDelete:
		value := strings.Trim(r.Header.Get("If-Match"), "\"")
		revision, err := strconv.Atoi(value)
		if err != nil || revision < 1 { writeError(w, 428, "If-Match recipe revision required"); return }
		if err := s.recipes.Delete(id, revision); err != nil { recipeFailure(w, err); return }
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) recipeNutrition(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil { writeError(w, 503, "recipe storage is not configured"); return }
	servings, err := strconv.ParseFloat(r.URL.Query().Get("servings"), 64)
	if err != nil { writeError(w, 400, "servings must be numeric"); return }
	preview, err := s.recipes.Nutrition(r.PathValue("id"), servings)
	if err != nil { recipeFailure(w, err); return }
	writeJSON(w, 200, preview)
}

func (s *Server) recipeArchive(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil { writeError(w, 503, "recipe storage is not configured"); return }
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Disposition", "attachment; filename=\"recipes.json\"")
		writeJSON(w, 200, s.recipes.Export())
		return
	}
	var doc recipes.Document
	if err := decode(r, &doc); err != nil { writeError(w, 400, err.Error()); return }
	count, err := s.recipes.Import(doc)
	if err != nil { recipeFailure(w, err); return }
	writeJSON(w, 201, map[string]int{"imported":count})
}

func (s *Server) mealCollection(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil { writeError(w, 503, "recipe storage is not configured"); return }
	if r.Method == http.MethodGet { writeJSON(w, 200, s.recipes.Meals()); return }
	var meal recipes.Meal
	if err := decode(r, &meal); err != nil { writeError(w, 400, err.Error()); return }
	created, err := s.recipes.AddMeal(meal)
	if err != nil { recipeFailure(w, err); return }
	writeJSON(w, 201, created)
}

func (s *Server) mealItem(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil { writeError(w, 503, "recipe storage is not configured"); return }
	if err := s.recipes.DeleteMeal(r.PathValue("id")); err != nil { recipeFailure(w, err); return }
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recipeGroceries(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil { writeError(w, 503, "recipe storage is not configured"); return }
	items, err := s.recipes.Groceries(r.URL.Query().Get("start"), r.URL.Query().Get("end"))
	if err != nil { recipeFailure(w, err); return }
	writeJSON(w, 200, items)
}

func (s *Server) recipeTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if s.recipes == nil { return nil, errors.New("recipes are not configured") }
	var input struct {
		ID       string         `json:"id"`
		Query    string         `json:"query"`
		URL      string         `json:"url"`
		Source   string         `json:"source"`
		Digest   string         `json:"digest"`
		Keys     []string       `json:"keys"`
		Overrides map[string]recipes.Recipe `json:"overrides"`
		Servings float64        `json:"servings"`
		Start    string         `json:"start"`
		End      string         `json:"end"`
		Recipe   recipes.Recipe `json:"recipe"`
		Meal     recipes.Meal   `json:"meal"`
		Revision int            `json:"revision"`
		SourceRevision int       `json:"source_revision"`
		ExpectedRevision int     `json:"expected_revision"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil { return nil, err }
	}
	switch name {
	case "recipe_import_url_preview": return recipes.FetchSchemaOrgURL(ctx, input.URL)
	case "recipe_sage_preview": return s.recipes.PreviewRecipeSage([]byte(input.Source))
	case "recipe_sage_import": return s.recipes.ImportRecipeSage([]byte(input.Source), input.Digest, input.Keys, input.Overrides)
	case "recipe_list": return s.recipes.List(input.Query), nil
	case "recipe_get": return s.recipes.Get(input.ID)
	case "recipe_history": return s.recipes.History(input.ID)
	case "recipe_restore": return s.recipes.Restore(input.ID, input.SourceRevision, input.ExpectedRevision)
	case "recipe_save": return s.recipes.Save(input.Recipe)
	case "recipe_delete": return map[string]bool{"deleted":true}, s.recipes.Delete(input.ID, input.Revision)
	case "recipe_meal_schedule": return s.recipes.AddMeal(input.Meal)
	case "recipe_meal_remove": return map[string]bool{"deleted":true}, s.recipes.DeleteMeal(input.ID)
	case "recipe_meals": return s.recipes.Meals(), nil
	case "recipe_groceries": return s.recipes.Groceries(input.Start, input.End)
	case "recipe_nutrition_preview": return s.recipes.Nutrition(input.ID, input.Servings)
	default: return nil, errors.New("unknown recipe tool")
	}
}

func (s *Server) recipeParse(w http.ResponseWriter,r *http.Request){
 var request struct{Source string `json:"source"`}
 if err:=decode(r,&request);err!=nil{writeError(w,400,err.Error());return}
 draft,err:=recipes.ParseSchemaOrg(request.Source)
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,200,draft)
}

func (s *Server) groceryBoard(w http.ResponseWriter,r *http.Request){
 if s.recipes==nil{writeError(w,503,"recipe storage is not configured");return}
 board,err:=s.recipes.Board(r.URL.Query().Get("start"),r.URL.Query().Get("end"))
 if err!=nil{recipeFailure(w,err);return}
 writeJSON(w,200,board)
}

func (s *Server) groceryUpdate(w http.ResponseWriter,r *http.Request){
 if s.recipes==nil{writeError(w,503,"recipe storage is not configured");return}
 var request struct{
  Checked bool `json:"checked"`
  ExpectedRevision int `json:"expected_revision"`
 }
 if err:=decode(r,&request);err!=nil{writeError(w,400,err.Error());return}
 board,err:=s.recipes.MarkGrocery(r.PathValue("key"),request.Checked,request.ExpectedRevision)
 if err!=nil{recipeFailure(w,err);return}
 writeJSON(w,200,board)
}

func (s *Server) recipeImageUpload(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mediaType != "image/png" && mediaType != "image/jpeg") {
		writeError(w, http.StatusUnsupportedMediaType, "JPEG or PNG image required")
		return
	}
	content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, recipes.MaxImageBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeError(w, http.StatusRequestEntityTooLarge, "recipe image exceeds 5 MiB")
		} else {
			writeError(w, http.StatusBadRequest, "could not read recipe image")
		}
		return
	}
	ref, err := s.recipes.PutImage(content, mediaType)
	switch {
	case errors.Is(err, recipes.ErrImageTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "recipe image exceeds 5 MiB")
	case errors.Is(err, recipes.ErrInvalidImage):
		writeError(w, http.StatusUnprocessableEntity, "image must be a valid JPEG or PNG under 16 megapixels")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "recipe image storage failed")
	default:
		writeJSON(w, http.StatusCreated, map[string]string{"image_ref": ref})
	}
}

func (s *Server) recipeImage(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	content, mediaType, err := s.recipes.GetImage(r.PathValue("name"))
	if errors.Is(err, recipes.ErrNotFound) {
		writeError(w, http.StatusNotFound, "recipe image not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "recipe image unavailable")
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) recipeImportURL(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	var request struct { URL string `json:"url"` }
	if err := decode(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid recipe URL request")
		return
	}
	draft, err := recipes.FetchSchemaOrgURL(r.Context(), request.URL)
	if err != nil {
		// Network error text can contain an internal URL, DNS result or transport
		// detail. Return one stable message without exposing those values.
		writeError(w, http.StatusUnprocessableEntity,
			"Recipe import failed. Check the HTTPS address and schema.org Recipe markup.")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}
