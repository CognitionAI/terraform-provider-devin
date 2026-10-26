# Slack, minimal: triage every new message in one channel with the default
# runbook, running as the organization.
resource "devin_oncall_responder" "payments" {
  org_id             = devin_organization.payments.org_id
  name               = "Payments oncall"
  triage_issue_type  = "alerts"
  slack_reply_access = "slack_users"

  triggers = jsonencode([{
    event_type = "slack:message"
    conditions = {
      any = [{ all = [{ field = "channel", operator = "eq", value = "C0123ABC" }] }]
    }
  }])
}

# Slack, complete: two channels plus a user-group mention (triggers are OR'd),
# a custom runbook, tools, limits, session settings, a cost-code tag and a
# service user identity.
resource "devin_oncall_responder" "checkout" {
  org_id            = devin_organization.payments.org_id
  name              = "Checkout oncall"
  triage_issue_type = "alerts"
  runbook           = file("${path.module}/runbooks/checkout.md")

  triggers = jsonencode([
    {
      event_type = "slack:message"
      conditions = {
        any = [{ all = [
          { field = "channel", operator = "in", value = ["C0INCIDENTS", "C0CHECKOUT"] },
          { field = "is_thread_reply", operator = "eq", value = false },
        ] }]
      }
    },
    {
      event_type = "slack:usergroup_mentioned"
      conditions = {
        any = [{ all = [
          { field = "channel", operator = "eq", value = "C0CHECKOUT" },
          { field = "usergroup", operator = "eq", value = "S0ONCALL" },
        ] }]
      }
    },
  ])

  tools = jsonencode({
    mcp_servers    = ["datadog", "sentry"]
    slack_channels = [{ team_id = "T0WORKSPACE", channel_id = "C0CHECKOUT" }]
    linear_enabled = true
  })

  limits = jsonencode({
    max_acu_limit = 20
    invocations   = { max_per_window = 30, window_seconds = 3600 }
  })

  session_settings = jsonencode({ devin_mode = "fast" })
  session          = jsonencode({ tags = ["oncall-checkout"] })

  slack_reply_access     = "devin_users"
  run_as                 = "service_user"
  run_as_service_user_id = var.oncall_service_user_id
  metadata               = { team = "checkout", owner = "payments-eng" }
}

# Microsoft Teams: every condition group names the team and its channels.
resource "devin_oncall_responder" "teams_platform" {
  org_id             = devin_organization.platform.org_id
  name               = "Platform oncall (Teams)"
  triage_issue_type  = "alerts"
  teams_reply_access = "teams_users"

  triggers = jsonencode([{
    event_type = "teams:message"
    conditions = {
      any = [{ all = [
        { field = "team_id", operator = "eq", value = "00000000-aaaa-bbbb-cccc-000000000000" },
        { field = "channel_id", operator = "in", value = ["19:abc@thread.tacv2", "19:def@thread.tacv2"] },
      ] }]
    }
  }])
}

# PagerDuty: triage P1 incidents of the payments service when they trigger,
# and again when they resolve to leave a summary.
data "devin_oncall_schemas" "payments" {
  org_id = devin_organization.payments.org_id
}

resource "devin_oncall_responder" "pd_payments" {
  org_id                  = devin_organization.payments.org_id
  name                    = "Payments P1 (PagerDuty)"
  triage_issue_type       = "alerts"
  pagerduty_connection_id = data.devin_oncall_schemas.payments.pagerduty_connection_id

  triggers = jsonencode([
    {
      event_type = "pagerduty:incident_triggered"
      conditions = {
        any = [{ all = [
          { field = "incident.service", operator = "eq", value = "payments-api" },
          { field = "incident.priority", operator = "eq", value = "P1" },
        ] }]
      }
    },
    { event_type = "pagerduty:incident_resolved" },
  ])
}

# Webhook: one webhook:incoming trigger and no conditions. Alerting systems
# POST to webhook_url with webhook_secret in the X-Webhook-Secret header.
resource "devin_oncall_responder" "grafana_alerts" {
  org_id            = devin_organization.platform.org_id
  name              = "Grafana alerts"
  triage_issue_type = "alerts"
  triggers          = jsonencode([{ event_type = "webhook:incoming" }])
}

output "grafana_webhook_url" {
  value = devin_oncall_responder.grafana_alerts.webhook_url
}

output "grafana_webhook_secret" {
  value     = devin_oncall_responder.grafana_alerts.webhook_secret
  sensitive = true
}

# Many teams with for_each: one responder per team from a single map.
locals {
  oncall_teams = {
    payments = { channel = "C0PAYMENTS", service_user = "service-user-payments" }
    search   = { channel = "C0SEARCH", service_user = "service-user-search" }
    infra    = { channel = "C0INFRA", service_user = "service-user-infra" }
  }
}

resource "devin_oncall_responder" "team" {
  for_each = local.oncall_teams

  org_id             = devin_organization.main.org_id
  name               = "${each.key} oncall"
  triage_issue_type  = "alerts"
  slack_reply_access = "slack_users"

  triggers = jsonencode([{
    event_type = "slack:message"
    conditions = {
      any = [{ all = [{ field = "channel", operator = "eq", value = each.value.channel }] }]
    }
  }])

  run_as                 = "service_user"
  run_as_service_user_id = each.value.service_user
  metadata               = { team = each.key }
}
