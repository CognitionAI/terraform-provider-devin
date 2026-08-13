resource "devin_snapshot_build" "after_blueprint" {
  org_id              = devin_blueprint.org_default.org_id
  wait_until_complete = true

  depends_on = [devin_blueprint.org_default]
}
