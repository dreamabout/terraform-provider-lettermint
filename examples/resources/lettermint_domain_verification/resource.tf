resource "lettermint_domain_verification" "example" {
  domain_id  = lettermint_domain.example.id
  depends_on = [cloudflare_dns_record.lettermint]

  timeouts = {
    create = "20m"
  }
}
