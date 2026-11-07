package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPGetter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small":
			w.Write([]byte("12345"))
		case "/big":
			w.Write([]byte(strings.Repeat("x", 11)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	get := httpGetter(10)
	if b, err := get(srv.URL + "/small"); err != nil || string(b) != "12345" {
		t.Errorf("small: %q %v", b, err)
	}
	if _, err := get(srv.URL + "/big"); err == nil || !strings.Contains(err.Error(), "larger than 10 bytes") {
		t.Errorf("over the limit: %v", err)
	}
	if _, err := get(srv.URL + "/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing: %v", err)
	}
	if b, err := httpGetter(5)(srv.URL + "/small"); err != nil || len(b) != 5 {
		t.Errorf("exactly the limit: %q %v", b, err)
	}
}
