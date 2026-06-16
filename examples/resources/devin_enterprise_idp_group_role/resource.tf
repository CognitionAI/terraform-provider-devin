# Look up role IDs instead of hard-coding them.
data "devin_roles" "all" {}

locals {
  enterprise_admin_role_id = one([
    for r in data.devin_roles.all.roles : r.role_id
    if r.role_type == "enterprise" && r.role_name == "Admin"
  ])
}

# Grant an enterprise-wide role to everyone in the IdP group.
resource "devin_enterprise_idp_group_role" "platform_admins" {
  idp_group_name = devin_idp_group.platform.name
  role_id        = local.enterprise_admin_role_id
}
