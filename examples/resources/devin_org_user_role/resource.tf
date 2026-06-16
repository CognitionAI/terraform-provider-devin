data "devin_roles" "all" {}

# Look up an existing enterprise user by email.
data "devin_users" "alice" {
  email = "alice@mycompany.com"
}

locals {
  org_member_role_id = one([
    for r in data.devin_roles.all.roles : r.role_id
    if r.role_type == "org" && r.role_name == "Member"
  ])
}

# Add the user to an organization with the given role. Destroying the
# resource removes the user from the organization.
resource "devin_org_user_role" "alice_backend" {
  org_id  = devin_organization.backend.org_id
  user_id = one(data.devin_users.alice.users).user_id
  role_id = local.org_member_role_id
}
