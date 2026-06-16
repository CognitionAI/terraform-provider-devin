data "devin_organizations" "all" {}

# Look up an existing org by name, e.g. to manage resources in it.
locals {
  platform_org_id = one([
    for o in data.devin_organizations.all.organizations : o.org_id
    if o.name == "Platform Team"
  ])
}
