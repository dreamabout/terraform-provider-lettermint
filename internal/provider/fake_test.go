package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeLettermint is an in-memory Team API with the endpoints the provider
// uses. Responses follow OpenAPI 0.0.1: DomainData has no status.
type fakeLettermint struct {
	*httptest.Server

	mu      sync.Mutex
	nextID  int
	domains map[string]*fakeDomain
	routes  map[string]*fakeRoute
	// verifyAfter is the number of failed verification calls before a domain
	// or inbound domain verifies.
	verifyAfter int
}

type fakeDomain struct {
	ID          string
	Name        string
	Status      string
	ProjectIDs  []string
	verifyCalls int
}

type fakeRoute struct {
	ID            string
	ProjectID     string
	Type          string
	Domain        *string
	VerifiedAt    *string
	SpamThreshold float64
	Attachment    string
	verifyCalls   int
}

func newFakeLettermint(t *testing.T) *fakeLettermint {
	f := &fakeLettermint{domains: map[string]*fakeDomain{}, routes: map[string]*fakeRoute{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeLettermint) addRoute(id, routeType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[id] = &fakeRoute{ID: id, ProjectID: "11111111-1111-1111-1111-111111111111", Type: routeType, SpamThreshold: 5, Attachment: "url"}
}

func (f *fakeLettermint) domainByName(name string) *fakeDomain {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.domains {
		if d.Name == name {
			return d
		}
	}
	return nil
}

func (f *fakeLettermint) route(id string) fakeRoute {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.routes[id]
}

func (f *fakeLettermint) records(d *fakeDomain) []map[string]any {
	rec := func(id, typ, host, content, purpose string, required bool) map[string]any {
		return map[string]any{
			"id": d.ID + "-" + id, "type": typ, "hostname": host, "fqdn": host + "." + d.Name,
			"content": content, "status": "pending", "purpose": purpose,
			"verification_scope": "required", "required_for_verification": required,
			"verified_at": nil, "last_checked_at": nil,
		}
	}
	return []map[string]any{
		rec("dkim", "TXT", "lm._domainkey", "v=DKIM1; k=rsa; p=abc", "dkim_legacy", true),
		rec("rp", "CNAME", "lm-bounces", "bounces.lmta.net", "return_path", true),
		rec("dmarc", "TXT", "_dmarc", "v=DMARC1; p=none", "dmarc", false),
	}
}

func (f *fakeLettermint) domainJSON(d *fakeDomain) map[string]any {
	projects := []map[string]any{}
	for _, p := range d.ProjectIDs {
		projects = append(projects, map[string]any{"id": p, "name": "Project " + p[:4]})
	}
	return map[string]any{
		"id": d.ID, "domain": d.Name, "dkim_mode": "legacy_txt", "rotation_ready": false,
		"status_changed_at": nil, "created_at": "2026-09-28T00:00:00Z",
		"dns_records": f.records(d), "projects": projects,
	}
}

func (f *fakeLettermint) routeJSON(r *fakeRoute) map[string]any {
	addr := r.ID + "@inbound.lettermint.co"
	return map[string]any{
		"id": r.ID, "project_id": r.ProjectID, "slug": "route-" + r.ID, "name": "Route " + r.ID,
		"route_type": r.Type, "is_default": false, "inbound_address": addr,
		"inbound_mx_hostname": "lettermint.mx", "inbound_domain": r.Domain,
		"inbound_domain_verified_at": r.VerifiedAt, "inbound_spam_threshold": r.SpamThreshold,
		"attachment_delivery": r.Attachment,
		"created_at":          "2026-09-28T00:00:00Z", "updated_at": "2026-09-28T00:00:00Z",
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeLettermint) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer lm_team_test" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthenticated."})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	notFound := func() { writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"}) }

	switch {
	case len(parts) == 1 && parts[0] == "domains" && r.Method == http.MethodGet:
		filter := strings.ToLower(r.URL.Query().Get("filter[domain]"))
		list := []map[string]any{}
		for _, d := range f.domains {
			if strings.Contains(d.Name, filter) {
				list = append(list, map[string]any{"id": d.ID, "domain": d.Name, "status": d.Status, "dkim_mode": "legacy_txt"})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": list, "next_cursor": nil})

	case len(parts) == 1 && parts[0] == "domains" && r.Method == http.MethodPost:
		var in struct{ Domain string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		in.Domain = strings.ToLower(in.Domain)
		for _, d := range f.domains {
			if d.Name == in.Domain {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"message": "The domain has already been taken.",
					"errors":  map[string][]string{"domain": {"The domain has already been taken."}},
				})
				return
			}
		}
		f.nextID++
		d := &fakeDomain{ID: fmt.Sprintf("0199aaaa-0000-7000-8000-%012d", f.nextID), Name: in.Domain, Status: "pending_verification"}
		f.domains[d.ID] = d
		writeJSON(w, http.StatusCreated, f.domainJSON(d))

	case len(parts) == 2 && parts[0] == "domains":
		d, ok := f.domains[parts[1]]
		if !ok {
			notFound()
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, f.domainJSON(d))
		case http.MethodDelete:
			delete(f.domains, d.ID)
			writeJSON(w, http.StatusOK, map[string]string{"message": "Domain deleted successfully."})
		}

	case len(parts) == 3 && parts[0] == "domains" && parts[2] == "projects" && r.Method == http.MethodPut:
		d, ok := f.domains[parts[1]]
		if !ok {
			notFound()
			return
		}
		var in struct {
			ProjectIDs []string `json:"project_ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		d.ProjectIDs = in.ProjectIDs
		writeJSON(w, http.StatusOK, map[string]any{"data": f.domainJSON(d), "message": "Domain projects updated successfully."})

	case len(parts) == 4 && parts[0] == "domains" && parts[2] == "dns-records" && parts[3] == "verify":
		d, ok := f.domains[parts[1]]
		if !ok {
			notFound()
			return
		}
		d.verifyCalls++
		if d.verifyCalls <= f.verifyAfter {
			d.Status = "failed_verification"
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message":        "Some required DNS records could not be verified.",
				"failed_records": []map[string]string{{"id": d.ID + "-dkim", "type": "TXT", "name": "lm._domainkey." + d.Name}},
			})
			return
		}
		d.Status = "verified"
		writeJSON(w, http.StatusOK, map[string]any{"message": "DNS records verified.", "recommended_failed_records": []any{}})

	case len(parts) == 2 && parts[0] == "routes":
		rt, ok := f.routes[parts[1]]
		if !ok {
			notFound()
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, f.routeJSON(rt))
		case http.MethodPut:
			var in struct {
				InboundSettings map[string]json.RawMessage `json:"inbound_settings"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if rt.Type != "inbound" && in.InboundSettings != nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Inbound settings are only accepted for inbound routes.", "errors": map[string][]string{}})
				return
			}
			if raw, ok := in.InboundSettings["inbound_domain"]; ok {
				var s *string
				_ = json.Unmarshal(raw, &s)
				if s != nil {
					lower := strings.ToLower(*s)
					s = &lower
				}
				if s == nil || rt.Domain == nil || *s != *rt.Domain {
					rt.VerifiedAt = nil
					rt.verifyCalls = 0
				}
				rt.Domain = s
			}
			if raw, ok := in.InboundSettings["inbound_spam_threshold"]; ok {
				_ = json.Unmarshal(raw, &rt.SpamThreshold)
			}
			if raw, ok := in.InboundSettings["attachment_delivery"]; ok {
				_ = json.Unmarshal(raw, &rt.Attachment)
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": f.routeJSON(rt), "message": "Route updated successfully."})
		}

	case len(parts) == 3 && parts[0] == "routes" && parts[2] == "verify-inbound-domain":
		rt, ok := f.routes[parts[1]]
		if !ok {
			notFound()
			return
		}
		rt.verifyCalls++
		if rt.Domain == nil || rt.verifyCalls <= f.verifyAfter {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"data": map[string]any{
				"verified": false, "message": "Inbound domain verification failed. Please ensure MX records point to our servers.",
				"expected_mx_records": []string{"lettermint.mx"}, "found_mx_records": []string{},
			}})
			return
		}
		now := "2026-09-28T12:00:00Z"
		rt.VerifiedAt = &now
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"verified": true, "message": "Inbound domain verified successfully."}})

	default:
		notFound()
	}
}

// providerConfig points the provider at the fake server.
func (f *fakeLettermint) providerConfig() string {
	return fmt.Sprintf(`
provider "lettermint" {
  token    = "lm_team_test"
  base_url = %q
}
`, f.URL)
}
