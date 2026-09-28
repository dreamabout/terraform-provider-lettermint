package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDomainUnwrapsDataAndIncludesRecords(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/domains/d1":
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"data":{"id":"d1","domain":"example.com","status":"verified","dkim_mode":"legacy_txt",
				"dns_records":[{"id":"r1","type":"TXT","hostname":"lm._domainkey","fqdn":"lm._domainkey.example.com","content":"v=DKIM1","purpose":"dkim_legacy","required_for_verification":true}],
				"projects":[{"id":"p1","name":"Shop"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d, err := New(srv.URL, "t", "").GetDomain(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "include=dnsRecords,projects" && gotQuery != "include=dnsRecords%2Cprojects" {
		t.Errorf("query = %q", gotQuery)
	}
	if d.ID != "d1" || d.Domain != "example.com" || d.Status != "verified" || d.DkimMode != "legacy_txt" {
		t.Errorf("domain = %+v", d)
	}
	if len(d.DNSRecords) != 1 || d.DNSRecords[0].FQDN != "lm._domainkey.example.com" || !d.DNSRecords[0].RequiredForVerification {
		t.Errorf("records = %+v", d.DNSRecords)
	}
	if len(d.Projects) != 1 || d.Projects[0].ID != "p1" {
		t.Errorf("projects = %+v", d.Projects)
	}
}

// DomainData in OpenAPI 0.0.1 has no status; only the list has it.
func TestGetDomainFallsBackToListForStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/domains/d1":
			_, _ = w.Write([]byte(`{"id":"d1","domain":"example.com","dkim_mode":"legacy_txt"}`))
		case "/domains":
			if r.URL.Query().Get("filter[domain]") != "example.com" {
				t.Errorf("filter = %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[
				{"id":"d2","domain":"sub.example.com","status":"verified"},
				{"id":"d1","domain":"example.com","status":"pending_verification"}],"next_cursor":null}`))
		}
	}))
	defer srv.Close()

	d, err := New(srv.URL, "t", "").GetDomain(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "pending_verification" {
		t.Errorf("status = %q", d.Status)
	}
}

func TestFindDomainByNameFollowsCursorAndMatchesExactly(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("page[cursor]") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"x","domain":"mail.example.com"}],"next_cursor":"c2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"d1","domain":"Example.com"}],"next_cursor":null}`))
	}))
	defer srv.Close()

	d, err := New(srv.URL, "t", "").FindDomainByName(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "d1" || calls != 2 {
		t.Errorf("id=%q calls=%d", d.ID, calls)
	}
}

func TestFindDomainByNameNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"next_cursor":null}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "t", "").FindDomainByName(context.Background(), "example.com")
	if !IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyDomainReportsFailedRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/domains/d1/dns-records/verify" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Verification failed","failed_records":[{"id":"r1","type":"TXT","name":"lm._domainkey.example.com"}]}`))
	}))
	defer srv.Close()

	res, err := New(srv.URL, "t", "").VerifyDomain(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified || len(res.FailedRecords) != 1 || res.FailedRecords[0].Name != "lm._domainkey.example.com" {
		t.Errorf("res = %+v", res)
	}
}

func TestVerifyDomainSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"ok","recommended_failed_records":[{"id":"r3","type":"TXT","name":"_dmarc.example.com"}]}`))
	}))
	defer srv.Close()

	res, err := New(srv.URL, "t", "").VerifyDomain(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Verified || len(res.RecommendedFailedRecords) != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestVerifyDomainOtherErrorsAreErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "t", "").VerifyDomain(context.Background(), "d1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDomainAndSetProjects(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		b, _ := json.Marshal(m)
		bodies = append(bodies, r.Method+" "+r.URL.Path+" "+string(b))
		switch r.URL.Path {
		case "/domains":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"d1","domain":"example.com"}`))
		case "/domains/d1/projects":
			_, _ = w.Write([]byte(`{"data":{"id":"d1","domain":"example.com","projects":[{"id":"p1","name":"x"}]},"message":"Domain projects updated successfully."}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "t", "")
	d, err := c.CreateDomain(context.Background(), "example.com")
	if err != nil || d.ID != "d1" {
		t.Fatalf("create: %v %+v", err, d)
	}
	if err := c.SetDomainProjects(context.Background(), "d1", []string{"p1"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`POST /domains {"domain":"example.com"}`,
		`PUT /domains/d1/projects {"project_ids":["p1"]}`,
	}
	for i := range want {
		if i >= len(bodies) || bodies[i] != want[i] {
			t.Errorf("request %d = %q, want %q", i, bodies, want[i])
		}
	}
}

func TestListDomainsFollowsCursor(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("page[cursor]") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"a","domain":"b.example.com","status":"verified","dkim_mode":"legacy_txt"}],"next_cursor":"c2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"b","domain":"a.example.com","status":"pending_verification","dkim_mode":"managed_cname"}],"next_cursor":null}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "t", "").ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].DkimMode != "managed_cname" || got[1].Status != "pending_verification" {
		t.Errorf("domains = %+v", got)
	}
	if len(queries) != 2 {
		t.Errorf("queries = %v", queries)
	}
}

func TestGetDomainReadsRotationReadyAndScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"d1","domain":"example.com","status":"verified","dkim_mode":"managed_cname","rotation_ready":true,
			"dns_records":[{"type":"TXT","fqdn":"lettermint._domainkey.example.com","purpose":"dkim_legacy","verification_scope":"deprecated"}]}`))
	}))
	defer srv.Close()

	d, err := New(srv.URL, "t", "").GetDomain(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if !d.RotationReady || d.DNSRecords[0].VerificationScope != "deprecated" {
		t.Errorf("domain = %+v", d)
	}
}
