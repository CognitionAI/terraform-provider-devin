terraform {
  required_providers {
    devin = {
      source = "registry.terraform.io/cognitionai/devin"
    }
  }
}

provider "devin" {
  # Token is read from the DEVIN_TOKEN environment variable by default.
  # See the Authentication section below for other options.
}
