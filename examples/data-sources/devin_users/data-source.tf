# All users in the enterprise.
data "devin_users" "all" {}

# Look up a single user by email, e.g. for devin_org_user_role.
data "devin_users" "alice" {
  email = "alice@mycompany.com"
}

output "alice_user_id" {
  value = one(data.devin_users.alice.users).user_id
}
