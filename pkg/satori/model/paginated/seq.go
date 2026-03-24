package paginated

import (
	"context"
	"errors"
	"iter"
	"strings"
	"sync"
)

var ErrNilPaginatedFetcher = errors.New("nil paginated fetcher")

type PaginatedFetcher[T any] func(ctx context.Context, nextToken string) (*Paginated[T], error)

// PaginatedSeq aligns with satori-python IterablePageResult.
// It can fetch one page directly and iterate all items across pages.
type PaginatedSeq[T any] struct {
	mu    sync.RWMutex
	fetch PaginatedFetcher[T]
	ctx   context.Context
	next  string
}

func NewPaginatedSeq[T any](ctx context.Context, nextToken string, fetch PaginatedFetcher[T]) *PaginatedSeq[T] {
	if ctx == nil {
		ctx = context.Background()
	}
	return &PaginatedSeq[T]{
		fetch: fetch,
		ctx:   ctx,
		next:  strings.TrimSpace(nextToken),
	}
}

func (r *PaginatedSeq[T]) SetContext(ctx context.Context) {
	if r == nil || ctx == nil {
		return
	}
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
}

func (r *PaginatedSeq[T]) Context() context.Context {
	if r == nil {
		return context.Background()
	}
	r.mu.RLock()
	ctx := r.ctx
	r.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (r *PaginatedSeq[T]) NextToken() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	next := r.next
	r.mu.RUnlock()
	return next
}

func (r *PaginatedSeq[T]) SetNextToken(nextToken string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.next = strings.TrimSpace(nextToken)
	r.mu.Unlock()
}

// Await fetches exactly one page from current next-token.
func (r *PaginatedSeq[T]) Await() (*Paginated[T], error) {
	if r == nil || r.fetch == nil {
		return nil, ErrNilPaginatedFetcher
	}
	return r.fetch(r.Context(), r.NextToken())
}

// Pages iterates pages from current next-token and updates internal token.
func (r *PaginatedSeq[T]) Pages() iter.Seq2[*Paginated[T], error] {
	return func(yield func(*Paginated[T], error) bool) {
		if r == nil || r.fetch == nil {
			_ = yield(nil, ErrNilPaginatedFetcher)
			return
		}

		token := r.NextToken()
		ctx := r.Context()
		for {
			page, err := r.fetch(ctx, token)
			if err != nil {
				_ = yield(nil, err)
				return
			}
			if page == nil {
				return
			}
			if !yield(page, nil) {
				return
			}

			token = strings.TrimSpace(page.Next)
			r.SetNextToken(token)
			if token == "" {
				return
			}
		}
	}
}

// Iter iterates all data items from current next-token and updates internal token.
func (r *PaginatedSeq[T]) Iter() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		if r == nil || r.fetch == nil {
			_ = yield(zero, ErrNilPaginatedFetcher)
			return
		}

		token := r.NextToken()
		ctx := r.Context()
		for {
			page, err := r.fetch(ctx, token)
			if err != nil {
				_ = yield(zero, err)
				return
			}
			if page == nil {
				return
			}
			for _, item := range page.Data {
				if !yield(item, nil) {
					return
				}
			}
			token = strings.TrimSpace(page.Next)
			r.SetNextToken(token)
			if token == "" {
				return
			}
		}
	}
}
