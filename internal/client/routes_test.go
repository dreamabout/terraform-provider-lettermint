package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/routes/r1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"r1","project_id":"p1","slug":"inbound","name":"Inbound","route_type":"inbound",
			"inbound_address":"abc@inbound.lettermint.co","inbound_mx_hostname":"lettermint.mx","inbound_domain":"support.example.com",
			"inbound_domain_verified_at":null,"inbound_spam_threshold":5,"attachment_delivery":"url"}}`))
	}))
	defer srv.Close()

	rt, err := New(srv.URL, "t", "").GetRoute(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.RouteType != "inbound" || rt.InboundMXHostname != "lettermint.mx" || rt.InboundDomain == nil || *rt.InboundDomain != "support.example.com" {
		t.Errorf("route = %+v", rt)
	}
	if rt.InboundSpamThreshold == nil || *rt.InboundSpamThreshold != 5 || rt.AttachmentDelivery != "url" {
		t.Errorf("route = %+v", rt)
	}
}

func TestUpdateRouteInboundSendsNullToClear(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/routes/r1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"data":{"id":"r1","route_type":"inbound"},"message":"Route updated successfully."}`))
	}))
	defer srv.Close()

	err := New(srv.URL, "t", "").UpdateRouteInbound(context.Background(), "r1", InboundSettings{ClearDomain: true})
	if err != nil {
		t.Fatal(err)
	}
	inbound, ok := got["inbound_settings"].(map[string]any)
	if !ok {
		t.Fatalf("body = %v", got)
	}
	if v, present := inbound["inbound_domain"]; !present || v != nil {
		t.Errorf("inbound_domain = %v (present=%v)", v, present)
	}
	if _, present := inbound["inbound_spam_threshold"]; present {
		t.Errorf("spam threshold sent although unset: %v", inbound)
	}
}

func TestUpdateRouteInboundSendsSetFields(t *testing.T) {
	var got map[string]map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"data":{"id":"r1"}}`))
	}))
	defer srv.Close()

	domain, threshold, delivery := "support.example.com", 4.5, "inline"
	err := New(srv.URL, "t", "").UpdateRouteInbound(context.Background(), "r1", InboundSettings{
		Domain: &domain, SpamThreshold: &threshold, AttachmentDelivery: &delivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	in := got["inbound_settings"]
	if in["inbound_domain"] != "support.example.com" || in["inbound_spam_threshold"] != 4.5 || in["attachment_delivery"] != "inline" {
		t.Errorf("inbound_settings = %v", in)
	}
}

func TestVerifyInboundDomain(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
		found  string
	}{
		{200, `{"data":{"verified":true,"message":"Inbound domain verified successfully."}}`, true, ""},
		{422, `{"data":{"verified":false,"message":"failed","expected_mx_records":["lettermint.mx"],"found_mx_records":["mx.other.net"]}}`, false, "mx.other.net"},
		{422, `{"data":{"verified":false,"message":"failed","expected_mx_records":["lettermint.mx"],"found_mx_records":"none"}}`, false, "none"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/routes/r1/verify-inbound-domain" {
				t.Errorf("%s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		res, err := New(srv.URL, "t", "").VerifyInboundDomain(context.Background(), "r1")
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.Verified != tc.want {
			t.Errorf("verified = %v, want %v", res.Verified, tc.want)
		}
		if tc.found != "" && (len(res.FoundMXRecords) != 1 || res.FoundMXRecords[0] != tc.found) {
			t.Errorf("found = %v", res.FoundMXRecords)
		}
	}
}
