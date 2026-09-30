# devin_schedule is deprecated. New scheduled work should be a devin_automation
# with a schedule:recurring trigger, e.g.:
#
# resource "devin_automation" "nightly_triage" {
#   org_id = devin_organization.backend.org_id
#   name   = "Nightly issue triage"
#   triggers = jsonencode([{
#     event_type = "schedule:recurring"
#     conditions = {
#       any = [{ all = [{
#         field    = "rrule"
#         operator = "recurrence"
#         value    = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;BYHOUR=6;BYMINUTE=0"
#       }] }]
#     }
#   }])
#   actions = jsonencode([{
#     type   = "start_session"
#     prompt = "Triage new GitHub issues, label them, and propose fixes for the easy ones."
#   }])
# }
#
# Existing devin_schedule resources keep working; the examples below are kept
# for organizations not yet migrated to Automations.

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
