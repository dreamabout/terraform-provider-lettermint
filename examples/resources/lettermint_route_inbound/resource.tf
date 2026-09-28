data "lettermint_route" "inbound" {
  id = var.route_id
}

resource "cloudflare_dns_record" "inbound_mx" {
  zone_id  = var.zone_id
  name     = "support.example.com"
  type     = "MX"
  content  = data.lettermint_route.inbound.inbound_mx_hostname
  priority = 10
  ttl      = 1
}

resource "lettermint_route_inbound" "support" {
  route_id            = var.route_id
  inbound_domain      = "support.example.com"
  spam_threshold      = 5
  attachment_delivery = "url"
  depends_on          = [cloudflare_dns_record.inbound_mx]
}
