# Enterprise-level notes are shared across every organization.
resource "devin_enterprise_knowledge_note" "security_policy" {
  name    = "Security Policy"
  body    = file("${path.module}/knowledge/security-policy.md")
  trigger = "When handling credentials or secrets"
}

# Notes can be pinned to a specific repository.
resource "devin_enterprise_knowledge_note" "release_process" {
  name        = "Release Process"
  body        = file("${path.module}/knowledge/release-process.md")
  trigger     = "When cutting a release"
  pinned_repo = "mycompany/platform"
}
