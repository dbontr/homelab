package recipesage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes = 8 << 20

// Client accesses RecipeSage's documented compatibility REST API.
type Client struct {
	base   string
	token  string
	email  string
	secret string
	mu     sync.Mutex
	http   *http.Client
}

type Config struct {
	URL       string
	Token     string
	Email     string
	Password  string
	AllowHTTP bool
	HTTP      *http.Client
}

func New(cfg Config) (*Client, error) {
	uri, err := url.Parse(cfg.URL)
	if err != nil || uri == nil || uri.Host == "" || (uri.Scheme != "https" && uri.Scheme != "http") || uri.RawQuery != "" || uri.Fragment != "" || uri.User != nil {
		return nil, errors.New("RECIPE_SAGE_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if uri.Scheme == "http" && !cfg.AllowHTTP {
		return nil, errors.New("HTTP RecipeSage URLs require RECIPE_SAGE_ALLOW_HTTP=true (use only on a trusted network)")
	}
	if cfg.Token == "" && (cfg.Email == "" || cfg.Password == "") {
		return nil, errors.New("set RECIPE_SAGE_TOKEN or both RECIPE_SAGE_EMAIL and RECIPE_SAGE_PASSWORD")
	}
	client := cfg.HTTP
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{base: strings.TrimRight(cfg.URL, "/"), token: cfg.Token, email: cfg.Email, secret: cfg.Password, http: client}, nil
}

// Request returns the JSON response and retries once after an expired session
// when credentials are available. It never forwards authorization on redirects.
func (c *Client) Request(ctx context.Context, method, path string, input any) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New("invalid API path")
	}
	var payload []byte
	if input != nil {
		var err error
		payload, err = json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}
	token, err := c.session(ctx)
	if err != nil {
		return nil, err
	}
	result, status, err := c.send(ctx, method, path, payload, token)
	if status == http.StatusUnauthorized && c.email != "" && c.secret != "" {
		c.mu.Lock()
		if c.token == token {
			c.token = ""
		}
		c.mu.Unlock()
		token, loginErr := c.session(ctx)
		if loginErr != nil {
			return nil, loginErr
		}
		result, _, err = c.send(ctx, method, path, payload, token)
	}
	return result, err
}

func (c *Client) session(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	payload, _ := json.Marshal(map[string]string{"email": c.email, "password": c.secret})
	data, _, err := c.send(ctx, http.MethodPost, "/compat/v2/users/login", payload, "")
	if err != nil {
		return "", fmt.Errorf("RecipeSage login failed: %w", err)
	}
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Token == "" {
		return "", errors.New("RecipeSage login did not return a token")
	}
	c.token = response.Token
	return c.token, nil
}

func (c *Client) send(ctx context.Context, method, path string, payload []byte, token string) (json.RawMessage, int, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, 0, fmt.Errorf("create upstream request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("RecipeSage request failed: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return nil, res.StatusCode, fmt.Errorf("read RecipeSage response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return nil, res.StatusCode, errors.New("RecipeSage response exceeds 8 MiB limit")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// Do not include the response body: upstream errors may contain sensitive data.
		return nil, res.StatusCode, fmt.Errorf("RecipeSage returned HTTP %d for %s", res.StatusCode, path)
	}
	if len(data) == 0 {
		return json.RawMessage(`{}`), res.StatusCode, nil
	}
	if !json.Valid(data) {
		return nil, res.StatusCode, errors.New("RecipeSage returned invalid JSON")
	}
	return data, res.StatusCode, nil
}

// RecipeUpdate reads the existing record and sends the complete update shape.
// This preserves fields omitted from a patch, including images and labels.
func (c *Client) RecipeUpdate(ctx context.Context, id string, patch map[string]json.RawMessage) (json.RawMessage, error) {
	if id == "" {
		return nil, errors.New("recipe id is required")
	}
	for field := range patch {
		if _, ok := editableRecipeFields[field]; !ok {
			return nil, fmt.Errorf("recipe field %q is not editable", field)
		}
	}
	existing, err := c.Request(ctx, http.MethodGet, "/compat/v2/recipes/getRecipe?id="+url.QueryEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var src map[string]json.RawMessage
	if err := json.Unmarshal(existing, &src); err != nil {
		return nil, fmt.Errorf("decode existing recipe: %w", err)
	}
	if src == nil {
		return nil, errors.New("RecipeSage returned an empty recipe")
	}
	record := make(map[string]json.RawMessage)
	for _, key := range recipeWriteFields {
		if raw, ok := src[key]; ok {
			record[key] = raw
		}
	}
	for _, key := range recipeRequiredStrings {
		if _, ok := record[key]; !ok || string(record[key]) == "null" {
			record[key] = json.RawMessage(`""`)
		}
	}
	if _, ok := record["rating"]; !ok {
		record["rating"] = json.RawMessage(`null`)
	}
	if _, ok := record["folder"]; !ok {
		record["folder"] = json.RawMessage(`"main"`)
	}
	record["id"], _ = json.Marshal(id)
	if _, ok := patch["labelIds"]; !ok {
		record["labelIds"] = nestedIDs(src["recipeLabels"], "labelId", "label", "id")
	}
	if _, ok := patch["imageIds"]; !ok {
		record["imageIds"] = nestedIDs(src["recipeImages"], "imageId", "image", "id")
	}
	if _, ok := patch["linkedRecipeIds"]; !ok {
		record["linkedRecipeIds"] = nestedIDs(src["recipeLinks"], "linkedRecipeId", "linkedRecipe", "id")
	}
	for key, value := range patch {
		record[key] = value
	}
	result, err := c.Request(ctx, http.MethodPost, "/compat/v2/recipes/updateRecipe", record)
	if err != nil {
		return nil, err
	}
	return result, nil
}

var recipeRequiredStrings = []string{"title", "description", "yield", "activeTime", "totalTime", "source", "url", "notes", "ingredients", "instructions"}
var recipeWriteFields = []string{
	"title", "description", "yield", "activeTime", "totalTime", "source", "url", "notes", "ingredients", "instructions", "rating", "folder", "lastMadeAt",
	"nutritionServingSize", "nutritionCalories", "nutritionTotalFat", "nutritionSaturatedFat", "nutritionTransFat", "nutritionPolyunsaturatedFat", "nutritionMonounsaturatedFat",
	"nutritionCholesterol", "nutritionSodium", "nutritionTotalCarbs", "nutritionDietaryFiber", "nutritionTotalSugars", "nutritionAddedSugars", "nutritionProtein",
	"nutritionVitaminD", "nutritionCalcium", "nutritionIron", "nutritionPotassium", "nutritionOtherDetails",
}

var editableRecipeFields = func() map[string]struct{} {
	m := make(map[string]struct{})
	for _, f := range recipeWriteFields {
		m[f] = struct{}{}
	}
	for _, f := range []string{"labelIds", "imageIds", "linkedRecipeIds"} {
		m[f] = struct{}{}
	}
	return m
}()

func nestedIDs(raw json.RawMessage, direct, nested, nestedKey string) json.RawMessage {
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return json.RawMessage(`[]`)
	}
	values := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		id := item[direct]
		if len(id) == 0 || string(id) == "null" {
			var child map[string]json.RawMessage
			_ = json.Unmarshal(item[nested], &child)
			id = child[nestedKey]
		}
		if len(id) > 0 && string(id) != "null" {
			values = append(values, id)
		}
	}
	out, _ := json.Marshal(values)
	return out
}

// PrepareRecipe builds the full write payload required by createRecipe.
func PrepareRecipe(fields map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if len(fields) == 0 {
		return nil, errors.New("recipe is required")
	}
	out := map[string]json.RawMessage{}
	for k, v := range fields {
		if k == "id" {
			return nil, errors.New("recipe creation must not include an id")
		}
		if _, ok := editableRecipeFields[k]; !ok {
			return nil, fmt.Errorf("recipe field %q is not supported", k)
		}
		out[k] = v
	}
	var title string
	if err := json.Unmarshal(out["title"], &title); err != nil || strings.TrimSpace(title) == "" {
		return nil, errors.New("recipe title is required")
	}
	for _, k := range recipeRequiredStrings {
		if _, ok := out[k]; !ok {
			out[k] = json.RawMessage(`""`)
		}
	}
	if _, ok := out["rating"]; !ok {
		out["rating"] = json.RawMessage(`null`)
	}
	if _, ok := out["folder"]; !ok {
		out["folder"] = json.RawMessage(`"main"`)
	}
	for _, k := range []string{"labelIds", "imageIds", "linkedRecipeIds"} {
		if _, ok := out[k]; !ok {
			out[k] = json.RawMessage(`[]`)
		}
	}
	return out, nil
}

// IsRecipeField reports whether a field belongs in a RecipeSage recipe write.
func IsRecipeField(name string) bool {
	_, ok := editableRecipeFields[name]
	return ok
}
