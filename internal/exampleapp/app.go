// Package exampleapp owns sample domain behaviour. It deliberately has no HTTP imports.
package exampleapp

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"sync"
)

type Item struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}
type Result struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

var ErrInvalid = errors.New("invalid business input")
var ErrConflict = errors.New("idempotency key reused for a different request")

type receipt struct {
	title string
	item  Item
}
type Store struct {
	mu    sync.Mutex
	items []Item
	keys  map[string]receipt
}

func NewStore() *Store { return &Store{keys: make(map[string]receipt)} }

func (s *Store) Create(ctx context.Context, title, key string) (Item, error) {
	if err := ctx.Err(); err != nil {
		return Item{}, err
	}
	if len(title) < 1 || len(title) > 200 {
		return Item{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != "" {
		if previous, exists := s.keys[key]; exists {
			if previous.title != title {
				return Item{}, ErrConflict
			}
			return previous.item, nil
		}
	}
	item := Item{ID: len(s.items) + 1, Title: title}
	s.items = append(s.items, item)
	if key != "" {
		s.keys[key] = receipt{title, item}
	}
	return item, nil
}

func (s *Store) List(ctx context.Context, limit int, cursor string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	after := 0
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return Result{}, ErrInvalid
		}
		n, err := strconv.Atoi(string(raw))
		if err != nil || n < 1 {
			return Result{}, ErrInvalid
		}
		after = n
		if cursor != base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(n))) {
			return Result{}, ErrInvalid
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || after > len(s.items) {
		return Result{}, ErrInvalid
	}
	end := after + limit
	if end > len(s.items) {
		end = len(s.items)
	}
	result := Result{Items: append([]Item{}, s.items[after:end]...)}
	if end < len(s.items) {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	return result, nil
}
