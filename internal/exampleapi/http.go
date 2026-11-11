// Package exampleapi translates HTTP requests into example application calls.
package exampleapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	api "github.com/jonathanselby/api-interface"
	"github.com/jonathanselby/api-interface/internal/exampleapp"
)

type Service interface {
	Create(context.Context, string, string) (exampleapp.Item, error)
	List(context.Context, int, string) (exampleapp.Result, error)
}

type API struct{ Handler *api.API }

func New(s Service, ready func(context.Context) error, trustedProxies []string) (*API, error) {
	var handler *api.API
	makeError := func(w http.ResponseWriter, r *http.Request, err error) {
		switch {
		case errors.Is(err, exampleapp.ErrInvalid):
			api.WriteError(w, r, api.Problem(api.Unprocessable))
		case errors.Is(err, exampleapp.ErrConflict):
			api.WriteError(w, r, api.Problem(api.Conflict))
		default:
			api.WriteError(w, r, err)
		}
	}
	routes := []api.Route{
		{
			Method: "POST", Path: "/api/v1/items", OperationID: "createItem", Summary: "Create an item",
			Request:   &api.JSONBody{Required: true, Schema: json.RawMessage(`{"type":"object","required":["title"],"properties":{"title":{"type":"string","minLength":1,"maxLength":200}},"additionalProperties":false}`)},
			Responses: []api.Response{{Status: 201, Description: "Created item", Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"},"title":{"type":"string"}},"required":["id","title"]}`)}, {Status: 400, Description: "Invalid request"}, {Status: 409, Description: "Conflict"}, {Status: 413, Description: "Request too large"}, {Status: 415, Description: "Unsupported media type"}, {Status: 422, Description: "Invalid operation"}},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var command struct {
					Title string `json:"title"`
				}
				if err := api.DecodeJSON(w, r, &command, 64<<10); err != nil {
					api.WriteError(w, r, err)
					return
				}
				key, err := api.IdempotencyKey(r)
				if err != nil {
					api.WriteError(w, r, err)
					return
				}
				result, err := s.Create(r.Context(), command.Title, key)
				if err != nil {
					makeError(w, r, err)
					return
				}
				api.WriteJSON(w, 201, result)
			}),
		},
		{
			Method: "GET", Path: "/api/v1/items", OperationID: "listItems", Summary: "List items",
			Parameters: []api.Parameter{
				{Name: "limit", In: "query", Schema: json.RawMessage(`{"type":"integer","minimum":1,"maximum":100}`)},
				{Name: "cursor", In: "query", Schema: json.RawMessage(`{"type":"string","maxLength":512}`)},
			},
			Responses: []api.Response{{Status: 200, Description: "Items in stable order", Schema: json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"integer"},"title":{"type":"string"}},"required":["id","title"]}},"next_cursor":{"type":"string"}},"required":["items"]}`)}, {Status: 400, Description: "Invalid request"}, {Status: 422, Description: "Invalid cursor"}},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, err := (api.Pagination{DefaultLimit: 20, MaxLimit: 100, MaxCursorBytes: 512}).Parse(r)
				if err != nil {
					api.WriteError(w, r, err)
					return
				}
				result, err := s.List(r.Context(), page.Limit, page.Cursor)
				if err != nil {
					makeError(w, r, err)
					return
				}
				api.WriteJSON(w, 200, result)
			}),
		},
	}
	handler, err := api.New(api.Config{Title: "API Interface Example", Version: "0.1.0", MaxBodyBytes: 64 << 10, Ready: ready, TrustedProxyCIDRs: trustedProxies}, routes)
	if err != nil {
		return nil, err
	}
	return &API{Handler: handler}, nil
}
