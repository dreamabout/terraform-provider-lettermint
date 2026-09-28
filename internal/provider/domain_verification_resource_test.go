package provider

import (
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func fastPolling(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })
}

func TestDomainVerificationResource(t *testing.T) {
	fastPolling(t)
	f := newFakeLettermint(t)
	f.verifyAfter = 2
	cfg := f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "example.com"
}

resource "lettermint_domain_verification" "test" {
  domain_id = lettermint_domain.test.id
}`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("lettermint_domain_verification.test", "domain_id", "lettermint_domain.test", "id"),
					resource.TestCheckResourceAttrPair("lettermint_domain_verification.test", "id", "lettermint_domain.test", "id"),
					resource.TestCheckResourceAttr("lettermint_domain_verification.test", "status", "verified"),
				),
			},
			{
				// Lettermint loses the verification: the resource is planned
				// again, so the next apply re-verifies.
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					for _, d := range f.domains {
						d.Status = "failed_verification"
					}
				},
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestDomainVerificationTimesOutNamingMissingRecords(t *testing.T) {
	fastPolling(t)
	f := newFakeLettermint(t)
	f.verifyAfter = 1 << 30

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "example.com"
}

resource "lettermint_domain_verification" "test" {
  domain_id = lettermint_domain.test.id
  timeouts = {
    create = "200ms"
  }
}`,
				ExpectError: regexp.MustCompile(`(?s)example\.com.*not verified.*TXT lm\._domainkey\.example\.com`),
			},
		},
	})
}
