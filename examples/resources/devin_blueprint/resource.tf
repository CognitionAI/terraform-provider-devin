resource "devin_blueprint" "org_default" {
  org_id   = var.org_id
  contents = <<-EOT
    version: 1
    initialize:
      - uses: github.com/CognitionAI/actions/setup-devin-oidc@main
    start:
      - name: shell
        command: bash
  EOT
}
