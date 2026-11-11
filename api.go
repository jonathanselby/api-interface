package apiinterface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Route binds the runtime handler and OpenAPI operation in the same registration.
type Route struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Parameters  []Parameter
	Request     *JSONBody
	Responses   []Response
	Handler     http.Handler
}

type Parameter struct {
	Name     string          `json:"name"`
	In       string          `json:"in"`
	Required bool            `json:"required"`
	Schema   json.RawMessage `json:"schema"`
}
type JSONBody struct {
	Required bool
	Schema   json.RawMessage
}
type Response struct {
	Status      int
	Description string
	Schema      json.RawMessage // nil for no body
}
type Config struct {
	Title             string
	Version           string
	MaxBodyBytes      int64
	TrustedProxyCIDRs []string
	Ready             func(context.Context) error
}
type API struct {
	mux          *http.ServeMux
	proxies      []*net.IPNet
	maxBodyBytes int64
	openapi      []byte
}

func (a *API) trustedPeer(ip net.IP) bool {
	for _, p := range a.proxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func validSchema(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var obj map[string]any
	return json.Unmarshal(raw, &obj) == nil && obj != nil
}

// New builds an immutable routing table and its matching OpenAPI document.
// Registering additional routes after serving is intentionally unsupported.
func New(config Config, routes []Route) (*API, error) {
	if config.Title == "" || config.Version == "" || config.MaxBodyBytes < 1 {
		return nil, errors.New("title, version and positive body limit required")
	}
	api := &API{mux: http.NewServeMux(), maxBodyBytes: config.MaxBodyBytes}
	for _, cidr := range config.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR: %w", err)
		}
		api.proxies = append(api.proxies, network)
	}
	seen := map[string]bool{}
	operationIDs := map[string]bool{}
	paths := map[string]any{}
	for _, route := range routes {
		if !validRouteMethod(route.Method) || !strings.HasPrefix(route.Path, "/api/v1/") || route.Handler == nil || route.OperationID == "" || route.Summary == "" || len(route.Responses) == 0 {
			return nil, fmt.Errorf("incomplete route %q", route.Path)
		}
		key := route.Method + " " + route.Path
		if seen[key] || operationIDs[route.OperationID] {
			return nil, fmt.Errorf("duplicate route or operation ID: %q", key)
		}
		seen[key], operationIDs[route.OperationID] = true, true
		params := []Parameter{}
		for _, p := range route.Parameters {
			if p.Name == "" || (p.In != "path" && p.In != "query" && p.In != "header") || !validSchema(p.Schema) || (p.In == "path" && !p.Required) {
				return nil, fmt.Errorf("invalid parameter for %q", key)
			}
			params = append(params, p)
		}
		// Native ServeMux path variables must be described in OpenAPI.
		for _, part := range strings.Split(route.Path, "/") {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
				found := false
				for _, p := range params {
					if p.Name == name && p.In == "path" {
						found = true
					}
				}
				if !found {
					return nil, fmt.Errorf("undocumented path parameter %q", name)
				}
			}
		}
		op := map[string]any{"operationId": route.OperationID, "summary": route.Summary, "responses": map[string]any{}}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if route.Request != nil {
			if !validSchema(route.Request.Schema) {
				return nil, fmt.Errorf("invalid request JSON schema for %q", key)
			}
			op["requestBody"] = map[string]any{"required": route.Request.Required, "content": map[string]any{"application/json": map[string]any{"schema": route.Request.Schema}}}
		}
		out := op["responses"].(map[string]any)
		for _, resp := range route.Responses {
			if resp.Status < 100 || resp.Status > 599 || resp.Description == "" {
				return nil, fmt.Errorf("invalid response for %q", key)
			}
			status := strconv.Itoa(resp.Status)
			if _, exists := out[status]; exists {
				return nil, fmt.Errorf("duplicate response status for %q", key)
			}
			doc := map[string]any{"description": resp.Description}
			if len(resp.Schema) > 0 {
				if !validSchema(resp.Schema) {
					return nil, fmt.Errorf("invalid response JSON schema for %q", key)
				}
				doc["content"] = map[string]any{"application/json": map[string]any{"schema": resp.Schema}}
			}
			out[status] = doc
		}
		pathItem, exists := paths[route.Path]
		if !exists {
			pathItem = map[string]any{}
			paths[route.Path] = pathItem
		}
		pathItem.(map[string]any)[strings.ToLower(route.Method)] = op
		api.mux.Handle(key, route.Handler)
	}
	spec := map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": config.Title, "version": config.Version}, "paths": paths}
	raw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}
	api.openapi = raw
	api.mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(append(api.openapi, '\n'))
	})
	api.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { WriteJSON(w, 200, map[string]string{"status": "ok"}) })
	api.mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if config.Ready != nil && config.Ready(r.Context()) != nil {
			WriteError(w, r, Problem(Unavailable))
			return
		}
		WriteJSON(w, 200, map[string]string{"status": "ready"})
	})
	return api, nil
}

func (a *API) OpenAPI() []byte { return append([]byte(nil), a.openapi...) }

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc, err := a.requestContext(r)
	req := r.WithContext(context.WithValue(r.Context(), requestContextKey{}, rc))
	w.Header().Set("X-Request-ID", rc.RequestID)
	if err != nil {
		WriteError(w, req, Problem(Invalid))
		return
	}
	if r.ContentLength > a.maxBodyBytes {
		WriteError(w, req, Problem(TooLarge))
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, a.maxBodyBytes)
	a.mux.ServeHTTP(w, req)
}
