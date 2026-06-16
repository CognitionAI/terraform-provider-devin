# IdP groups registered with the enterprise, e.g. to adopt existing groups
# without importing each devin_idp_group resource.
data "devin_idp_groups" "all" {}

output "idp_group_names" {
  value = [for g in data.devin_idp_groups.all.idp_groups : g.idp_group_name]
}
