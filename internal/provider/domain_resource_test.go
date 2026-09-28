package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestDomainResource(t *testing.T) {
	f := newFakeLettermint(t)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if n := len(f.domains); n != 0 {
				return fmt.Errorf("%d domains left", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "example.com"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("lettermint_domain.test", "id"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "status", "pending_verification"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dkim_mode", "legacy_txt"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "project_ids.#", "0"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.#", "3"),
					// Sorted by purpose, then fqdn.
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.0.purpose", "dkim_legacy"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.0.type", "TXT"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.0.hostname", "lm._domainkey"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.0.fqdn", "lm._domainkey.example.com"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.0.content", "v=DKIM1; k=rsa; p=abc"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.0.required_for_verification", "true"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.1.purpose", "dmarc"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.1.required_for_verification", "false"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "dns_records.2.purpose", "return_path"),
					func(s *terraform.State) error {
						firstID = s.RootModule().Resources["lettermint_domain.test"].Primary.ID
						return nil
					},
				),
			},
			{
				ResourceName:      "lettermint_domain.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "lettermint_domain.test",
				ImportState:       true,
				ImportStateId:     "example.com",
				ImportStateVerify: true,
			},
			{
				Config: f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain      = "example.com"
  project_ids = ["22222222-2222-2222-2222-222222222222"]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lettermint_domain.test", "project_ids.#", "1"),
					resource.TestCheckTypeSetElemAttr("lettermint_domain.test", "project_ids.*", "22222222-2222-2222-2222-222222222222"),
					func(*terraform.State) error {
						d := f.domainByName("example.com")
						if d == nil || len(d.ProjectIDs) != 1 {
							return fmt.Errorf("projects not set in Lettermint: %+v", d)
						}
						return nil
					},
				),
			},
			{
				Config: f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "mail.example.com"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lettermint_domain.test", "domain", "mail.example.com"),
					func(s *terraform.State) error {
						if s.RootModule().Resources["lettermint_domain.test"].Primary.ID == firstID {
							return fmt.Errorf("domain change did not replace the resource")
						}
						if f.domainByName("example.com") != nil {
							return fmt.Errorf("old domain still exists")
						}
						return nil
					},
				),
			},
		},
	})
}

func TestDomainResourceImportUnknownName(t *testing.T) {
	f := newFakeLettermint(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "nowhere.example.com"
}`,
				ResourceName:  "lettermint_domain.test",
				ImportState:   true,
				ImportStateId: "nowhere.example.com",
				ExpectError:   regexp.MustCompile(`no domain named nowhere.example.com`),
			},
		},
	})
}

func TestDomainResourceRemovedOutsideTerraform(t *testing.T) {
	f := newFakeLettermint(t)
	cfg := f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "example.com"
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					for id := range f.domains {
						delete(f.domains, id)
					}
				},
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Lettermint stores names in lower case; the configured case must not show
// up as a change.
func TestNamesKeepConfiguredCase(t *testing.T) {
	fastPolling(t)
	f := newFakeLettermint(t)
	f.addRoute("r1", "inbound")
	cfg := f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "Example.COM"
}

resource "lettermint_route_inbound" "test" {
  route_id       = "r1"
  inbound_domain = "Support.Example.com"
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lettermint_domain.test", "domain", "Example.COM"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "inbound_domain", "Support.Example.com"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}
