resource "devin_playbook" "coding_standards" {
  org_id = devin_organization.frontend.org_id
  title  = "Coding Standards"
  body   = file("${path.module}/playbooks/coding-standards.md")
}

# Playbooks can be invoked with a macro from the Devin chat input.
resource "devin_playbook" "onboarding" {
  org_id = devin_organization.frontend.org_id
  title  = "Onboarding"
  body   = file("${path.module}/playbooks/onboarding.md")
  macro  = "!onboard"
}
