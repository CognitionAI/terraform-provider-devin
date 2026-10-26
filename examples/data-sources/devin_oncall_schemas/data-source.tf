data "devin_oncall_schemas" "this" {
  org_id = devin_organization.payments.org_id
}

# Trigger event types available to the organization, e.g.
# ["pagerduty:incident_triggered", ..., "slack:message", "slack:usergroup_mentioned"].
output "oncall_event_types" {
  value = sort(flatten([for source in values(jsondecode(data.devin_oncall_schemas.this.sources)) : keys(source)]))
}

# A PagerDuty responder takes the account's PagerDuty connection from here.
resource "devin_oncall_responder" "pd_payments" {
  org_id                  = devin_organization.payments.org_id
  name                    = "Payments P1 (PagerDuty)"
  pagerduty_connection_id = data.devin_oncall_schemas.this.pagerduty_connection_id
  triggers                = jsonencode([{ event_type = "pagerduty:incident_triggered" }])
}
