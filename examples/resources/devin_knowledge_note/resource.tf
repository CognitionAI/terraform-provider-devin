resource "devin_knowledge_note" "api_conventions" {
  org_id  = devin_organization.backend.org_id
  name    = "API Conventions"
  body    = file("${path.module}/knowledge/api-conventions.md")
  trigger = "When working on API endpoints"
}

# Notes can be pinned to a specific repository.
resource "devin_knowledge_note" "deploy_process" {
  org_id      = devin_organization.backend.org_id
  name        = "Deploy Process"
  body        = file("${path.module}/knowledge/deploy-process.md")
  trigger     = "When deploying the service"
  pinned_repo = "mycompany/backend-service"
}
