package siteapi

import "net/http"

// RegisterHandlers registers any site-specific handlers for the mature httpserver.
// The current implementation is intentionally minimal so the shared backend and product
// entrypoints can evolve independently without introducing a hard dependency on the
// new product-layer package.
func RegisterHandlers(mux *http.ServeMux) {
	if mux == nil {
		return
	}
}
