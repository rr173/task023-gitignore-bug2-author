// Package httpapi 提供 gitignore 规则匹配引擎的 HTTP 接口。
// 服务无内部可变状态，可被多个 goroutine 复用。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"task023-gitignore/internal/gitignore"
)

// ErrBadJSON 表示请求体不是单个合法 JSON 对象。
var ErrBadJSON = errors.New("请求体不是合法的单个 JSON 对象")

// API 是 gitignore 服务的 HTTP 接口实现。
type API struct{}

// New 创建服务实例。
func New() *API { return &API{} }

// Handler 返回 HTTP 路由。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /check", a.check)
	return mux
}

// decodeJSON 解码单个 JSON 对象，拒绝多段 JSON 与未知字段。
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return ErrBadJSON
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return ErrBadJSON
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// errBody 用于各类 400 错误的统一回应。
type errBody struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// checkRequest 是判定端点的请求体。
type checkRequest struct {
	Rules string   `json:"rules"`
	Paths []string `json:"paths"`
}

// checkResponse 是判定端点的回应。
type checkResponse struct {
	Results []gitignore.Result `json:"results"`
	Ignored []string           `json:"ignored"`
	Kept    []string           `json:"kept"`
}

func (a *API) check(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{OK: false, Error: err.Error()})
		return
	}

	// 规范化路径：剥离前导 /，空路径或纯 / 视为非法。
	normalized := make([]string, 0, len(req.Paths))
	for _, p := range req.Paths {
		np := strings.TrimLeft(p, "/")
		if np == "" {
			writeJSON(w, http.StatusBadRequest, errBody{OK: false, Error: "存在空路径字符串"})
			return
		}
		normalized = append(normalized, np)
	}

	patterns, err := gitignore.Parse(req.Rules)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{OK: false, Error: err.Error()})
		return
	}

	results := gitignore.Check(patterns, normalized)
	if results == nil {
		results = []gitignore.Result{} // 空结果用 [] 而非 null
	}
	ignored := make([]string, 0, len(results))
	kept := make([]string, 0, len(results))
	for _, res := range results {
		if res.Ignored {
			ignored = append(ignored, res.Path)
		} else {
			kept = append(kept, res.Path)
		}
	}
	writeJSON(w, http.StatusOK, checkResponse{Results: results, Ignored: ignored, Kept: kept})
}
