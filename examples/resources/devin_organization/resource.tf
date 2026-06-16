resource "devin_organization" "frontend" {
  name                  = "Frontend Team"
  max_session_acu_limit = 200
}

resource "devin_organization" "backend" {
  name                  = "Backend Team"
  max_session_acu_limit = 500
  max_cycle_acu_limit   = 2000
}
