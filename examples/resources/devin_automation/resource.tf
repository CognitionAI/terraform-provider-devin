# A scheduled automation: starts a session every weekday morning.
resource "devin_automation" "nightly_triage" {
  org_id = devin_organization.frontend.org_id
  name   = "Nightly bug triage"
  triggers = jsonencode([{
    event_type = "schedule:recurring"
    conditions = {
      any = [{ all = [{
        field    = "rrule"
        operator = "recurrence"
        value    = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;BYHOUR=9;BYMINUTE=0"
      }] }]
    }
  }])
  actions = jsonencode([{
    type   = "start_session"
    prompt = "Triage any new bugs filed overnight and post a summary."
  }])
  limits = jsonencode({
    max_acu_limit = 25
  })
  metadata = {
    team = "frontend"
  }
}

# An event-driven automation: reviews dependency PRs and posts the session's
# final response back on the pull request.
resource "devin_automation" "dependency_prs" {
  org_id = devin_organization.frontend.org_id
  name   = "Review dependency bump PRs"
  triggers = jsonencode([{
    event_type = "github:pull_request"
    conditions = {
      any = [{ all = [
        { field = "repo", operator = "equals", value = "acme/frontend" },
        { field = "author", operator = "equals", value = "dependabot[bot]" },
      ] }]
    }
    replies = [{ type = "post_response" }]
  }])
  actions = jsonencode([{
    type   = "start_session"
    prompt = "Review this dependency bump for breaking changes."
  }])
}

# A webhook-triggered automation: external systems POST to webhook_url with
# the minted secret in the X-Webhook-Secret header.
resource "devin_automation" "incident_hook" {
  org_id   = devin_organization.frontend.org_id
  name     = "Incident intake"
  triggers = jsonencode([{ event_type = "webhook:incoming" }])
  actions = jsonencode([{
    type   = "start_session"
    prompt = "Investigate the incident described in the webhook payload."
  }])
}

output "incident_hook_url" {
  value = devin_automation.incident_hook.webhook_url
}
