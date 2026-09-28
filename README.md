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

## Udvikling

```bash
go vet ./...
golangci-lint run
go test ./...
```

Testene med `resource.UnitTest` kører Terraform-binæren (sæt
`TF_ACC_TERRAFORM_PATH`, hvis `terraform` ikke ligger på `PATH`). Mod
OpenTofu fejler terraform-plugin-testing i dag på provider-adressen.

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
