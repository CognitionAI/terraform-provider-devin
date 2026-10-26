# Oncall responders are imported with the owning org ID and the responder ID.
# The JSON-encoded groups (triggers, tools, ...) are config-authoritative and
# are not recovered by import; the first plan after import restates them. The
# webhook_secret of an imported webhook responder stays empty: the API only
# returns it when it is minted.
terraform import devin_oncall_responder.payments org-e2a868a8a12a4c53809dea41fbf7ee99/auto-24364480e58149da814b3aa2b3d0aa31
