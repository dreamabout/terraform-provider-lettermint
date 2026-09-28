package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var testProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"lettermint": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProviderSchemaIsValid(t *testing.T) {
	ctx := context.Background()
	server, err := testProtoV6ProviderFactories["lettermint"]()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("diagnostic: %s: %s", d.Summary, d.Detail)
	}
	attrs := map[string]bool{}
	for _, a := range resp.Provider.Block.Attributes {
		attrs[a.Name] = a.Sensitive
	}
	if sensitive, ok := attrs["token"]; !ok || !sensitive {
		t.Errorf("token attribute missing or not sensitive: %v", attrs)
	}
	if _, ok := attrs["base_url"]; !ok {
		t.Errorf("base_url attribute missing: %v", attrs)
	}
}

func TestResolveConfig(t *testing.T) {
	cases := []struct {
		name               string
		attrToken, envTok  string
		attrURL, envURL    string
		wantToken, wantURL string
		wantErr            bool
	}{
		{name: "env token, default url", envTok: "lm_team_env", wantToken: "lm_team_env", wantURL: "https://api.lettermint.co/v1"},
		{name: "attribute wins over env", attrToken: "lm_team_attr", envTok: "lm_team_env", wantToken: "lm_team_attr", wantURL: "https://api.lettermint.co/v1"},
		{name: "base_url attribute", envTok: "x", attrURL: "http://localhost:8080/v1", wantToken: "x", wantURL: "http://localhost:8080/v1"},
		{name: "base_url env", envTok: "x", envURL: "http://env/v1", wantToken: "x", wantURL: "http://env/v1"},
		{name: "no token", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LETTERMINT_TOKEN", tc.envTok)
			t.Setenv("LETTERMINT_BASE_URL", tc.envURL)
			token, url, err := resolveConfig(tc.attrToken, tc.attrURL)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if token != tc.wantToken || url != tc.wantURL {
				t.Errorf("got (%q, %q), want (%q, %q)", token, url, tc.wantToken, tc.wantURL)
			}
		})
	}
}

// A configuration with only the provider block plans without touching the
// API, so this runs without a token.
func TestProviderPlansWithoutResources(t *testing.T) {
	t.Setenv("LETTERMINT_TOKEN", "lm_team_unused")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             `provider "lettermint" {}`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}
