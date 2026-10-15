# Applied to every organization in the enterprise.
resource "devin_enterprise_blueprint" "this" {
  contents = file("${path.module}/blueprints/enterprise.yaml")
}
