# terraform-provider-lettermint

OpenTofu- og Terraform-provider til [Lettermints](https://lettermint.co) Team API:
domæner med DNS-poster og indgående routes. Udgives som `dreamabout/lettermint`.

## Brug

```hcl
terraform {
  required_providers {
    lettermint = {
      source = "dreamabout/lettermint"
    }
  }
}

provider "lettermint" {}
```

| Attribut | Miljøvariabel | Standard |
|---|---|---|
| `token` | `LETTERMINT_TOKEN` | — (Team API-token, `lm_team_…`) |
| `base_url` | `LETTERMINT_BASE_URL` | `https://api.lettermint.co/v1` |

Et token med `read:*` er nok til plan; apply kræver `write:*`.

## Ressourcer og datakilder

| Navn | Hvad |
|---|---|
| `lettermint_domain` | Et afsenderdomæne. `dns_records` er de poster, Lettermint kræver. Import på id eller domænenavn |
| `lettermint_domain_verification` | Venter, til Lettermint har verificeret domænets DNS-poster. Mister domænet sin verifikation, planlægges den igen |
| `lettermint_route_inbound` | En eksisterende inbound-routes `inbound_domain`, `spam_threshold` og `attachment_delivery`. Opretter og sletter ikke routen; destroy nulstiller `inbound_domain`. Import på route-id |
| `data.lettermint_domain` | Et domæne slået op på `id` eller `domain` |
| `data.lettermint_domains` | Alle domæner i teamet, også underdomæner, fx til en `check`-blok, der fanger domæner, konfigurationen ikke styrer |
| `data.lettermint_route` | En route, fx for `inbound_mx_hostname` |

Et domæne med DNS i Cloudflare:

```hcl
resource "lettermint_domain" "shop" {
  domain = "example.com"
}

resource "cloudflare_dns_record" "lettermint" {
  for_each = { for r in lettermint_domain.shop.dns_records : "${r.type} ${r.fqdn}" => r }

  zone_id = var.zone_id
  name    = each.value.fqdn
  type    = each.value.type
  content = each.value.content
  ttl     = 1
}

resource "lettermint_domain_verification" "shop" {
  domain_id  = lettermint_domain.shop.id
  depends_on = [cloudflare_dns_record.lettermint]
}
```

`dns_records` kendes først, når domænet findes, så et nyt domæne kræver to
applies (eller `-target=lettermint_domain.shop` først). Et importeret domæne
kræver kun ét.

Et inbound-domæne:

```hcl
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
  route_id       = var.route_id
  inbound_domain = "support.example.com"
  depends_on     = [cloudflare_dns_record.inbound_mx]
}
```

`dkim_mode` er `legacy_txt` eller `managed_cname`. Skiftet sker i Lettermints
dashboard; Team API'et har intet felt til det. Ved næste refresh følger
`dns_records` med, og `verification_scope` (`required`, `recommended`,
`migration` eller `deprecated`) skelner de gamle poster fra de nye.

`verify` (standard `true`) venter, til Lettermint har set MX-posten.
Verifikationen giver op efter 10 minutter; sæt `timeouts = { create = "20m" }`
for at vente længere.

## Udvikling

```bash
go vet ./...
golangci-lint run
go test ./...
```

Unit-testene kører provideren mod en fake Lettermint-server
(`internal/provider/fake_test.go`). Testene med `resource.UnitTest` kører Terraform-binæren (sæt
`TF_ACC_TERRAFORM_PATH`, hvis `terraform` ikke ligger på `PATH`). Mod
OpenTofu fejler terraform-plugin-testing i dag på provider-adressen.

Acceptance-testene (`TestAcc*`) kører mod et separat Lettermint-testteam og en
Cloudflare-zone med `TF_ACC=1`. Se kommentaren øverst i
`internal/provider/acc_test.go` for miljøvariablerne. I CI kører de i miljøet
`acceptance`.

Prøv en lokal build med `dev_overrides`:

```hcl
# ~/.tofurc eller filen i TF_CLI_CONFIG_FILE
provider_installation {
  dev_overrides {
    "dreamabout/lettermint" = "/sti/til/go/bin"
  }
  direct {}
}
```

```bash
go install .
cd examples/provider && tofu plan
```

Lettermint Team API: [OpenAPI 0.0.1](https://lettermint.co/docs/api-reference/team/0.0.1/lettermint-team-openapi.json).

Repoet er offentligt: ingen tokens eller kundedata i tests og eksempler.

## Dokumentation

`docs/` genereres af [tfplugindocs](https://github.com/hashicorp/terraform-plugin-docs)
ud fra skemaets beskrivelser og `examples/`. Ret dem dér, og kør:

```bash
go generate ./...
```

CI fejler, hvis `docs/` ikke svarer til koden.

## Udgivelse

Et tag `v*` udløser `.github/workflows/release.yml`: GoReleaser bygger en zip
pr. OS og arkitektur, `SHA256SUMS`, en GPG-signatur af den og registrets
manifest, og lægger dem på en GitHub-release. Terraform- og OpenTofu-registret
henter nye versioner derfra.

```bash
git tag v0.1.0
git push origin v0.1.0
```

Engangsopsætning:

1. **GPG-nøgle** (RSA). Den private nøgle og passphrasen som secrets
   `GPG_PRIVATE_KEY` og `GPG_PASSPHRASE` i repoets miljø `release`.
2. **Terraform-registret:** log ind med GitHub på registry.terraform.io, udgiv
   provideren fra dette repo, og læg den offentlige nøgle på namespace
   `dreamabout`.
3. **OpenTofu-registret:** anmeld provideren og den offentlige nøgle via
   issue-formularerne i [opentofu/registry](https://github.com/opentofu/registry/issues/new/choose).

Prøv udgivelsen lokalt uden at signere:

```bash
goreleaser release --snapshot --clean --skip=sign
```

