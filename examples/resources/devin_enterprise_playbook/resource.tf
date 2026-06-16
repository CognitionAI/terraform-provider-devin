# Enterprise-level playbooks are shared across every organization.
resource "devin_enterprise_playbook" "incident_response" {
  title = "Incident Response"
  body  = file("${path.module}/playbooks/incident-response.md")
  macro = "!incident"
}
