package web

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/web-fetch-http", NewWebFetchHTTP)
	pluginkit.Register("tool/web-search-exa", NewWebSearchExa)
	pluginkit.Register("tool/web-fetch-scripted", NewWebFetchScripted)
	pluginkit.Register("tool/web-search-scripted", NewWebSearchScripted)
}
