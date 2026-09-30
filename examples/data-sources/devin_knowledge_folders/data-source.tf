data "devin_knowledge_folders" "all" {
  org_id = var.org_id
}

resource "devin_knowledge_note" "backend" {
  org_id    = var.org_id
  name      = "Backend knowledge"
  body      = "Knowledge for the backend team."
  trigger   = "When working on backend code"
  folder_id = data.devin_knowledge_folders.all.folder_ids_by_path["Engineering/Backend"]
}
