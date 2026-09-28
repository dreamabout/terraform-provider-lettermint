package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestRouteInboundResource(t *testing.T) {
	fastPolling(t)
	f := newFakeLettermint(t)
	f.verifyAfter = 1
	f.addRoute("r1", "inbound")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			// Destroy clears the inbound domain and keeps the route.
			if rt := f.route("r1"); rt.Domain != nil {
				return fmt.Errorf("inbound_domain still %q", *rt.Domain)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_route_inbound" "test" {
  route_id       = "r1"
  inbound_domain = "support.example.com"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "id", "r1"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "inbound_domain", "support.example.com"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "inbound_mx_hostname", "lettermint.mx"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "inbound_address", "r1@inbound.lettermint.co"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "spam_threshold", "5"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "attachment_delivery", "url"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "verify", "true"),
					resource.TestCheckResourceAttrSet("lettermint_route_inbound.test", "inbound_domain_verified_at"),
				),
			},
			{
				ResourceName:            "lettermint_route_inbound.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				Config: f.providerConfig() + `
resource "lettermint_route_inbound" "test" {
  route_id            = "r1"
  inbound_domain      = "help.example.com"
  spam_threshold      = 3.5
  attachment_delivery = "inline"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "inbound_domain", "help.example.com"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "spam_threshold", "3.5"),
					resource.TestCheckResourceAttr("lettermint_route_inbound.test", "attachment_delivery", "inline"),
					resource.TestCheckResourceAttrSet("lettermint_route_inbound.test", "inbound_domain_verified_at"),
					func(*terraform.State) error {
						rt := f.route("r1")
						if rt.Domain == nil || *rt.Domain != "help.example.com" || rt.SpamThreshold != 3.5 || rt.Attachment != "inline" {
							return fmt.Errorf("route in Lettermint = %+v", rt)
						}
						return nil
					},
				),
			},
			{
				// Removing inbound_domain from the configuration clears it.
				Config: f.providerConfig() + `
resource "lettermint_route_inbound" "test" {
  route_id = "r1"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("lettermint_route_inbound.test", "inbound_domain"),
					resource.TestCheckNoResourceAttr("lettermint_route_inbound.test", "inbound_domain_verified_at"),
					func(*terraform.State) error {
						if rt := f.route("r1"); rt.Domain != nil {
							return fmt.Errorf("inbound_domain still %q", *rt.Domain)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestRouteInboundWithoutVerify(t *testing.T) {
	f := newFakeLettermint(t)
	f.verifyAfter = 1 << 30
	f.addRoute("r1", "inbound")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_route_inbound" "test" {
  route_id       = "r1"
  inbound_domain = "support.example.com"
  verify         = false
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("lettermint_route_inbound.test", "inbound_domain_verified_at"),
					func(*terraform.State) error {
						if n := f.route("r1").verifyCalls; n != 0 {
							return fmt.Errorf("verify called %d times", n)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestRouteInboundVerifyTimesOut(t *testing.T) {
	fastPolling(t)
	f := newFakeLettermint(t)
	f.verifyAfter = 1 << 30
	f.addRoute("r1", "inbound")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_route_inbound" "test" {
  route_id       = "r1"
  inbound_domain = "support.example.com"
  timeouts = {
    create = "200ms"
  }
}`,
				ExpectError: regexp.MustCompile(`(?s)support\.example\.com.*not verified.*lettermint\.mx`),
			},
		},
	})
}

func TestRouteInboundRejectsOtherRouteTypes(t *testing.T) {
	f := newFakeLettermint(t)
	f.addRoute("r2", "transactional")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.providerConfig() + `
resource "lettermint_route_inbound" "test" {
  route_id       = "r2"
  inbound_domain = "support.example.com"
}`,
				ExpectError: regexp.MustCompile(`route r2 is a transactional route`),
			},
		},
	})
}
