terraform {
  required_providers {
    lettermint = {
      source = "dreamabout/lettermint"
    }
  }
}

# The token is read from LETTERMINT_TOKEN when not set here.
provider "lettermint" {}
