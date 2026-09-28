package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingSendsBearerToken(t *testing.T) {
	var gotAuth, gotPath, gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("200"))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "lm_team_abc", "test/1.0")
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotAuth != "Bearer lm_team_abc" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotPath != "/v1/ping" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotUA != "test/1.0" {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestBaseURLWithTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("200"))
	}))
	defer srv.Close()

	if err := New(srv.URL+"/v1/", "t", "").Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotPath != "/v1/ping" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestErrorCarriesStatusAndMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"The domain field is required.","errors":{"domain":["The domain field is required."]}}`))
	}))
	defer srv.Close()

	err := New(srv.URL, "t", "").Ping(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Message != "The domain field is required." {
		t.Errorf("Message = %q", apiErr.Message)
	}
	if got := apiErr.Errors["domain"]; len(got) != 1 {
		t.Errorf("Errors[domain] = %v", got)
	}
}

func TestIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not found"}`))
	}))
	defer srv.Close()

	err := New(srv.URL, "t", "").Ping(context.Background())
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound(%v) = false", err)
	}
	if IsNotFound(errors.New("other")) {
		t.Fatal("IsNotFound(other) = true")
	}
}

func TestErrorWithoutJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer srv.Close()

	err := New(srv.URL, "t", "").Ping(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("err = %v", err)
	}
}

func TestDoDecodesJSON(t *testing.T) {
	var gotMethod, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"d1","domain":"example.com"}`))
	}))
	defer srv.Close()

	var out struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	}
	in := map[string]string{"domain": "example.com"}
	if err := New(srv.URL, "t", "").Do(context.Background(), http.MethodPost, "/domains", in, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotMethod != http.MethodPost || gotContentType != "application/json" {
		t.Errorf("method=%s content-type=%s", gotMethod, gotContentType)
	}
	if gotBody != `{"domain":"example.com"}` {
		t.Errorf("body = %s", gotBody)
	}
	if out.ID != "d1" || out.Domain != "example.com" {
		t.Errorf("out = %+v", out)
	}
}
