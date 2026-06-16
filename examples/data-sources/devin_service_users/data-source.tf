# All active service users in the enterprise, e.g. to look up IDs for
# devin_enterprise_service_user_role or devin_org_service_user_role.
data "devin_service_users" "all" {}

output "service_user_ids" {
  value = {
    for su in data.devin_service_users.all.service_users : su.name => su.service_user_id
  }
}
