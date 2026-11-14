package media

import (
	"bytes"
	"context"
    "crypto/sha256"
    "encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client can search and request media, but cannot administer Seerr.
type Client struct {
	base *url.URL
	key  string
 keySource func()(string,error)
 scopedKeySource func(context.Context,string,string,json.RawMessage)(string,error)
	http *http.Client
}

func NewSeerr(raw, apiKey string) (*Client, error) {
 return newSeerr(raw,apiKey,nil,nil)
}

// NewSeerrWithSource takes the key from a trusted service identity on each
// request. A failed source never falls back to a previously cached key.
func NewSeerrWithSource(raw string,source func()(string,error))(*Client,error){
 if source==nil{return nil,errors.New("Seerr credential source required")}
 return newSeerr(raw,"",source,nil)
}

// NewSeerrWithScopedSource redeems a one-use vault ticket for each
// authenticated media action. There is no ambient provider credential.
func NewSeerrWithScopedSource(raw string,source func(context.Context,string,string,json.RawMessage)(string,error))(*Client,error){
 if source==nil{return nil,errors.New("Seerr owner authorization source required")}
 return newSeerr(raw,"",nil,source)
}

func newSeerr(raw,apiKey string,source func()(string,error),
 scoped func(context.Context,string,string,json.RawMessage)(string,error),
)(*Client,error){
 sources:=0
 if strings.TrimSpace(apiKey)!=""{sources++}
 if source!=nil{sources++}
 if scoped!=nil{sources++}
 if sources!=1{return nil,errors.New("exactly one Seerr credential authority is required")}
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("Seerr URL must be an origin without credentials")
	}
	if u.Hostname() == "" || u.Port() == "" {
		return nil, errors.New("Seerr URL requires host and port")
	}
	if port, err := strconv.Atoi(u.Port()); err != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid Seerr port")
	}
	if u.Scheme != "https" {
		if u.Scheme != "http" {
			return nil, errors.New("Seerr requires HTTPS or private HTTP")
		}
		ip, err := netip.ParseAddr(u.Hostname())
		if err != nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
			return nil, errors.New("private HTTP requires a private or loopback IP address")
		}
	}
	return &Client{
		base: u,
		key: apiKey,
  keySource: source,
  scopedKeySource:scoped,
		http: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("Seerr redirects are disabled")
			},
		},
	}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	if c == nil {
		return nil, errors.New("media integration not configured")
	}
	dest := *c.base
	if i := strings.IndexByte(path, '?'); i >= 0 {
		dest.Path = path[:i]
		dest.RawQuery = path[i+1:]
	} else {
		dest.Path = path
		dest.RawQuery = ""
	}
	var payload io.Reader
    var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
        bodyBytes=b
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, dest.String(), payload)
	if err != nil {
		return nil, err
	}
 key:=c.key
 if c.scopedKeySource!=nil {
  digest:=sha256.Sum256(bodyBytes)
  scope,marshalErr:=json.Marshal(map[string]string{
   "method":method,"path":path,
   "body_sha256":hex.EncodeToString(digest[:]),
  })
  if marshalErr!=nil{return nil,errors.New("invalid Seerr credential scope")}
  var tokenErr error
  key,tokenErr=c.scopedKeySource(
   ctx,"media__seerr_request","seerr",scope)
  if tokenErr!=nil{return nil,errors.New("Seerr owner authorization denied")}
 }else if c.keySource!=nil{
  key,err=c.keySource()
  if err!=nil{return nil,errors.New("Seerr credential unavailable")}
 }
 if key==""||strings.ContainsAny(key,"\x00\r\n\t ")||
  (c.keySource!=nil||c.scopedKeySource!=nil)&&len(key)<20{
  return nil,errors.New("invalid Seerr credential")
 }
 req.Header.Set("X-Api-Key",key)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Seerr request failed with HTTP %d", resp.StatusCode)
	}
	if !json.Valid(data) {
		return nil, errors.New("Seerr returned invalid JSON")
	}
	return json.RawMessage(data), nil
}

func (c *Client) Search(ctx context.Context, query string) (json.RawMessage, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 200 {
		return nil, errors.New("query must contain 1-200 characters")
	}
	return c.request(ctx, http.MethodGet, "/api/v1/search?query="+url.QueryEscape(query), nil)
}

func (c *Client) Requests(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/request?take=25&skip=0", nil)
}

type MediaRequest struct {
	MediaType string `json:"mediaType"`
	MediaID   int    `json:"mediaId"`
	Seasons   []int  `json:"seasons,omitempty"`
	Is4K      bool   `json:"is4k"`
}

func (c *Client) AddRequest(ctx context.Context, request MediaRequest) (json.RawMessage, error) {
	if request.MediaType != "movie" && request.MediaType != "tv" {
		return nil, errors.New("media type must be movie or tv")
	}
	if request.MediaID <= 0 {
		return nil, errors.New("media ID must be positive")
	}
	if request.MediaType == "movie" && len(request.Seasons) > 0 {
		return nil, errors.New("movies cannot specify seasons")
	}
	for _, season := range request.Seasons {
		if season < 0 || season > 200 {
			return nil, errors.New("invalid season number")
		}
	}
	return c.request(ctx, http.MethodPost, "/api/v1/request", request)
}
