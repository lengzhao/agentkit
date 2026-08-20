package web

import "context"

type Service interface {
	Search(context.Context, SearchRequest) (SearchResult, error)
	Fetch(context.Context, FetchRequest) (FetchResult, error)
}

type SearchRequest struct{}
type SearchResult struct{}
type FetchRequest struct{}
type FetchResult struct{}
