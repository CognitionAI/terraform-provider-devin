# Maven settings available to the blueprint's steps through the file's
# env_var_name.
resource "devin_blueprint_file" "maven_settings" {
  org_id         = devin_organization.frontend.org_id
  blueprint_id   = devin_blueprint.org.blueprint_id
  name           = "settings.xml"
  content_base64 = filebase64("${path.module}/files/settings.xml")
}
