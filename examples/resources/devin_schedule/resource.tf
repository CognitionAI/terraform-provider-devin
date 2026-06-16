# Recurring schedule: run every weekday morning.
resource "devin_schedule" "nightly_triage" {
  org_id    = devin_organization.backend.org_id
  name      = "Nightly issue triage"
  prompt    = "Triage new GitHub issues, label them, and propose fixes for the easy ones."
  frequency = "0 6 * * 1-5"
  notify_on = "failure"
}

# One-time schedule: kick off a session at a specific time.
resource "devin_schedule" "release_prep" {
  org_id        = devin_organization.backend.org_id
  name          = "Release prep"
  prompt        = "Prepare the changelog and release notes for the upcoming release."
  schedule_type = "one_time"
  scheduled_at  = "2030-01-15T09:00:00Z"
}
