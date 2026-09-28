data "lettermint_domains" "all" {}

# Warn at plan when Lettermint has a domain this configuration does not manage.
# lettermint_domain.managed is a for_each keyed by domain name.
check "lettermint_domains_managed" {
  assert {
    condition = alltrue([
      for d in data.lettermint_domains.all.domains : contains(keys(lettermint_domain.managed), d.domain)
    ])
    error_message = "Lettermint has domains that are not managed here: ${join(", ", [
      for d in data.lettermint_domains.all.domains : d.domain if !contains(keys(lettermint_domain.managed), d.domain)
    ])}"
  }
}
