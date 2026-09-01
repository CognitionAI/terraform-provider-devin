# Knowledge folders in an organization. Devin has no API for creating,
# renaming, or deleting folders, so they are created in the Devin UI; this
# data source resolves one to the folder_id a knowledge note needs.
data "devin_knowledge_folders" "backend" {
  org_id = devin_organization.backend.org_id
}

locals {
  # Look the folder up by its path so the opaque ID stays out of the config.
  conventions_folder_id = one([
    for f in data.devin_knowledge_folders.backend.folders : f.folder_id
    if f.path == "Engineering/Conventions"
  ])
}

resource "devin_knowledge_note" "api_conventions" {
  org_id    = devin_organization.backend.org_id
  name      = "API Conventions"
  body      = file("${path.module}/knowledge/api-conventions.md")
  trigger   = "When working on API endpoints"
  folder_id = local.conventions_folder_id
}

output "knowledge_folder_paths" {
  value = [for f in data.devin_knowledge_folders.backend.folders : f.path]
}

# Notes that are not filed under any folder.
output "unfiled_note_count" {
  value = data.devin_knowledge_folders.backend.root_note_count
}
