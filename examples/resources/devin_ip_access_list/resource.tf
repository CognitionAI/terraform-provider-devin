# Restrict enterprise access to the office network. This is an enterprise
# singleton: declare at most one, and applying it replaces the whole list.
resource "devin_ip_access_list" "office" {
  ip_ranges = ["203.0.113.0/24", "198.51.100.0/24"]
}
