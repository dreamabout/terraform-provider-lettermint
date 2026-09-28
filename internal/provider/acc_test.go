package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

// Acceptance tests run against a real Lettermint team and a Cloudflare zone
// with TF_ACC=1 and:
//
//	LETTERMINT_TOKEN         Team API token with read:* and write:*
//	LETTERMINT_ACC_ROUTE_ID  an inbound route the tests may change
//	LETTERMINT_ACC_DOMAIN    a (sub)domain in the zone, such as tf-acc.example.com
//	LETTERMINT_ACC_ZONE_ID   the Cloudflare zone id
//	CLOUDFLARE_API_TOKEN     DNS:Edit on the zone
//
// Each run uses random names under LETTERMINT_ACC_DOMAIN and removes what it
// created.
type accEnv struct {
	routeID, domain, zoneID string
}

func accPreCheck(t *testing.T) accEnv {
	t.Helper()
	env := accEnv{
		routeID: os.Getenv("LETTERMINT_ACC_ROUTE_ID"),
		domain:  os.Getenv("LETTERMINT_ACC_DOMAIN"),
		zoneID:  os.Getenv("LETTERMINT_ACC_ZONE_ID"),
	}
	for name, v := range map[string]string{
		"LETTERMINT_TOKEN":        os.Getenv("LETTERMINT_TOKEN"),
		"LETTERMINT_ACC_ROUTE_ID": env.routeID,
		"LETTERMINT_ACC_DOMAIN":   env.domain,
		"LETTERMINT_ACC_ZONE_ID":  env.zoneID,
		"CLOUDFLARE_API_TOKEN":    os.Getenv("CLOUDFLARE_API_TOKEN"),
	} {
		if v == "" && os.Getenv("TF_ACC") != "" {
			t.Fatalf("%s must be set for acceptance tests", name)
		}
	}
	return env
}

var cloudflareProvider = map[string]resource.ExternalProvider{
	"cloudflare": {Source: "cloudflare/cloudflare", VersionConstraint: "~> 5.0"},
}

func accClient() *client.Client {
	token, baseURL, _ := resolveConfig("", "")
	return client.New(baseURL, token, "terraform-provider-lettermint/acctest")
}

func TestAccDomainLifecycle(t *testing.T) {
	env := accPreCheck(t)
	suffix := strings.ToLower(acctest.RandStringFromCharSet(8, acctest.CharSetAlpha))
	domain := "d-" + suffix + "." + env.domain

	domainOnly := fmt.Sprintf(`
provider "lettermint" {}

resource "lettermint_domain" "test" {
  domain = %q
}
`, domain)

	// dns_records is unknown until the domain exists, so the records are
	// added in a second step (for_each needs known keys).
	withDNS := domainOnly + fmt.Sprintf(`
provider "cloudflare" {}

resource "cloudflare_dns_record" "lettermint" {
  for_each = { for r in lettermint_domain.test.dns_records : "${r.type} ${r.fqdn}" => r }

  zone_id = %q
  name    = each.value.fqdn
  type    = each.value.type
  content = each.value.content
  ttl     = 1
}

resource "lettermint_domain_verification" "test" {
  domain_id  = lettermint_domain.test.id
  depends_on = [cloudflare_dns_record.lettermint]
}
`, env.zoneID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		ExternalProviders:        cloudflareProvider,
		CheckDestroy: func(s *terraform.State) error {
			if _, err := accClient().FindDomainByName(context.Background(), domain); !client.IsNotFound(err) {
				return fmt.Errorf("domain %s still exists (err=%w)", domain, err)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: domainOnly,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("lettermint_domain.test", "id"),
					resource.TestCheckResourceAttr("lettermint_domain.test", "domain", domain),
					resource.TestCheckResourceAttrSet("lettermint_domain.test", "dns_records.0.fqdn"),
					resource.TestCheckResourceAttrSet("lettermint_domain.test", "dkim_mode"),
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
				ImportStateId:     domain,
				ImportStateVerify: true,
			},
			{
				Config: withDNS,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("lettermint_domain_verification.test", "domain_id", "lettermint_domain.test", "id"),
					resource.TestCheckResourceAttrWith("lettermint_domain_verification.test", "status", func(v string) error {
						if v != "verified" && v != "partially_verified" {
							return fmt.Errorf("status = %q", v)
						}
						return nil
					}),
				),
			},
			{
				// Verification changes no planned attribute of the domain.
				Config:   withDNS,
				PlanOnly: true,
			},
		},
	})
}

func TestAccRouteInbound(t *testing.T) {
	env := accPreCheck(t)
	suffix := strings.ToLower(acctest.RandStringFromCharSet(8, acctest.CharSetAlpha))
	inbound := "in-" + suffix + "." + env.domain

	withDomain := fmt.Sprintf(`
provider "lettermint" {}
provider "cloudflare" {}

data "lettermint_route" "test" {
  id = %[1]q
}

resource "cloudflare_dns_record" "mx" {
  zone_id  = %[2]q
  name     = %[3]q
  type     = "MX"
  content  = data.lettermint_route.test.inbound_mx_hostname
  priority = 10
  ttl      = 1
}

resource "lettermint_route_inbound" "test" {
  route_id       = %[1]q
  inbound_domain = %[3]q
  depends_on     = [cloudflare_dns_record.mx]
}
`, env.routeID, env.zoneID, inbound)

	cleared := fmt.Sprintf(`
provider "lettermint" {}

resource "lettermint_route_inbound" "test" {
  route_id = %q
}
`, env.routeID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		ExternalProviders:        cloudflareProvider,
		CheckDestroy: func(*terraform.State) error {
			rt, err := accClient().GetRoute(context.Background(), env.routeID)
			if err != nil {
				return err
			}
			if rt.InboundDomain != nil {
				return fmt.Errorf("route %s still has inbound_domain %q", env.routeID, *rt.InboundDomain)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: withDomain,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "inbound_domain", inbound),
					resource.TestCheckResourceAttrSet("lettermint_route_inbound.test", "inbound_domain_verified_at"),
					resource.TestCheckResourceAttrSet("lettermint_route_inbound.test", "inbound_mx_hostname"),
					resource.TestCheckResourceAttrSet("lettermint_route_inbound.test", "spam_threshold"),
					resource.TestCheckResourceAttrSet("lettermint_route_inbound.test", "attachment_delivery"),
				),
			},
			{
				ResourceName:            "lettermint_route_inbound.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				Config: cleared,
				Check:  resource.TestCheckNoResourceAttr("lettermint_route_inbound.test", "inbound_domain"),
			},
		},
	})
}
