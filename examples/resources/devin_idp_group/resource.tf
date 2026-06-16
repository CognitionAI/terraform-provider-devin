# Register an IdP (SSO) group name so roles can be mapped to it.
resource "devin_idp_group" "engineering" {
  name = "engineering"
}
