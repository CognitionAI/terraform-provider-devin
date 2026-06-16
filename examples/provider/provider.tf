terraform {
  required_providers {
    devin = {
      source = "registry.terraform.io/cognitionai/devin"
    }
  }
}

provider "devin" {
  # api_url = "https://api.devin.ai"   # or set DEVIN_API_URL
  # token   = "cog_..."                 # or set DEVIN_TOKEN
}
