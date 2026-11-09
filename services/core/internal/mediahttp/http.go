package mediahttp

import (
 "net/http"

 "github.com/dbontr/homelab-new/system/internal/httpjson"
 "github.com/dbontr/homelab-new/system/media"
)

type Handler struct{client *media.Client}

// Register exposes only Seerr search and request operations. The media stack
// and its persistent request database are independent of every interface.
func Register(mux *http.ServeMux,client *media.Client){
 if mux==nil{panic("media requires an HTTP router")}
 h:=&Handler{client:client}
 mux.HandleFunc("GET /v1/media/search",h.search)
 mux.HandleFunc("GET /v1/media/requests",h.requests)
 mux.HandleFunc("POST /v1/media/requests",h.requests)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpjson.Error(w, http.StatusServiceUnavailable, "media is not configured")
		return
	}
	data, err := h.client.Search(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		httpjson.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, data)
}

func (h *Handler) requests(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpjson.Error(w, http.StatusServiceUnavailable, "media is not configured")
		return
	}
	if r.Method == http.MethodGet {
		data, err := h.client.Requests(r.Context())
		if err != nil {
			httpjson.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		httpjson.Write(w, http.StatusOK, data)
		return
	}
	var input media.MediaRequest
	if err := httpjson.Decode(r, &input); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := h.client.AddRequest(r.Context(), input)
	if err != nil {
		httpjson.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpjson.Write(w, http.StatusCreated, data)
}

