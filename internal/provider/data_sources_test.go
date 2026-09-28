package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestDomainDataSource(t *testing.T) {
	f := newFakeLettermint(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_domain" "test" {
  domain = "example.com"
}

data "lettermint_domain" "by_name" {
  domain     = "example.com"
  depends_on = [lettermint_domain.test]
}

data "lettermint_domain" "by_id" {
  id = lettermint_domain.test.id
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.lettermint_domain.by_name", "id", "lettermint_domain.test", "id"),
					resource.TestCheckResourceAttr("data.lettermint_domain.by_name", "status", "pending_verification"),
					resource.TestCheckResourceAttr("data.lettermint_domain.by_name", "dns_records.#", "3"),
					resource.TestCheckResourceAttr("data.lettermint_domain.by_name", "dns_records.0.fqdn", "lm._domainkey.example.com"),
					resource.TestCheckResourceAttr("data.lettermint_domain.by_id", "domain", "example.com"),
					resource.TestCheckResourceAttr("data.lettermint_domain.by_id", "dkim_mode", "legacy_txt"),
				),
			},
		},
	})
}

func TestDomainDataSourceNeedsExactlyOneKey(t *testing.T) {
	f := newFakeLettermint(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
data "lettermint_domain" "test" {}`,
				ExpectError: regexp.MustCompile(`(?i)exactly one of`),
			},
		},
	})
}

func TestRouteDataSource(t *testing.T) {
	f := newFakeLettermint(t)
	f.addRoute("r1", "inbound")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
data "lettermint_route" "test" {
  id = "r1"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.lettermint_route.test", "route_type", "inbound"),
					resource.TestCheckResourceAttr("data.lettermint_route.test", "project_id", "11111111-1111-1111-1111-111111111111"),
					resource.TestCheckResourceAttr("data.lettermint_route.test", "slug", "route-r1"),
					resource.TestCheckResourceAttr("data.lettermint_route.test", "name", "Route r1"),
					resource.TestCheckResourceAttr("data.lettermint_route.test", "inbound_mx_hostname", "lettermint.mx"),
					resource.TestCheckResourceAttr("data.lettermint_route.test", "inbound_address", "r1@inbound.lettermint.co"),
					resource.TestCheckNoResourceAttr("data.lettermint_route.test", "inbound_domain"),
				),
			},
		},
	})
}
