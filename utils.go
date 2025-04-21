package a2a

import (
	"net/http"
	"strings"
)

func Ptr[T any](v T) *T {
	return &v
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.EqualFold(proto, "https")
	}
	if fwd := r.Header.Get("Forwarded"); strings.Contains(strings.ToLower(fwd), "proto=https") {
		return true
	}
	return false
}
