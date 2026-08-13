# Automations are imported with the owning org ID and the automation ID.
# The JSON-encoded groups (triggers, actions, ...) are config-authoritative
# and are not recovered by import; the first plan after import restates them.
terraform import devin_automation.nightly_triage org-e2a868a8a12a4c53809dea41fbf7ee99/automation-24364480e58149da814b3aa2b3d0aa31
