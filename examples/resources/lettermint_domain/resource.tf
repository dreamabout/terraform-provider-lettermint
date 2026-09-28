resource "lettermint_domain" "example" {
  domain = "example.com"
}

# Create the records Lettermint needs, here in Cloudflare.
resource "cloudflare_dns_record" "lettermint" {
  for_each = { for r in lettermint_domain.example.dns_records : "${r.type} ${r.fqdn}" => r }

  zone_id = var.zone_id
  name    = each.value.fqdn
  type    = each.value.type
  content = each.value.content
  ttl     = 1
}
