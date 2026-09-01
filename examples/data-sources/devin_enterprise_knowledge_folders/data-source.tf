# Enterprise-level knowledge folders. Note that devin_enterprise_knowledge_note
# does not accept a folder_id — the enterprise notes endpoint rejects it — so
# this data source is for inspecting the enterprise tree. Use
# devin_knowledge_folders to place org-level notes.
data "devin_enterprise_knowledge_folders" "all" {}

output "enterprise_knowledge_folders" {
  value = {
    for f in data.devin_enterprise_knowledge_folders.all.folders : f.path => f.note_count
  }
}
