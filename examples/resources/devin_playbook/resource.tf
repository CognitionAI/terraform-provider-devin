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

# Playbooks can require sessions to produce structured output matching a
# JSON Schema (Draft 7), supplied as a JSON-encoded string.
resource "devin_playbook" "triage" {
  org_id = devin_organization.frontend.org_id
  title  = "Bug Triage"
  body   = file("${path.module}/playbooks/triage.md")
  structured_output_schema = jsonencode({
    type = "object"
    properties = {
      severity = {
        type = "string"
        enum = ["low", "medium", "high"]
      }
      summary = {
        type = "string"
      }
    }
    required = ["severity", "summary"]
  })
}
