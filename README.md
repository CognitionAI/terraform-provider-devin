# Terraform Provider for Devin

Manage Devin resources (organizations, git permissions, playbooks, knowledge notes, secrets, schedules, IP access lists, IdP group role mappings) via the [Devin v3 API](https://docs.devin.ai/api-reference/overview).

Authenticates with enterprise or organization-level service user tokens (`cog_` prefix).

## Usage

```hcl
terraform {
  required_providers {
    devin = {
      source = "registry.terraform.io/cognitionai/devin"
    }
  }
}

provider "devin" {
  # Token is read from the DEVIN_TOKEN environment variable by default.
  # See "Authentication" below.
}

data "devin_roles" "all" {}

resource "devin_organization" "team" {
  name                  = "My Team"
  max_session_acu_limit = 200
}

resource "devin_git_permission" "repos" {
  org_id            = devin_organization.team.org_id
  git_connection_id = "gc-abc123"
  group_prefix      = "mycompany/team"
}

resource "devin_playbook" "standards" {
  org_id = devin_organization.team.org_id
  title  = "Coding Standards"
  body   = file("playbooks/standards.md")
}

resource "devin_knowledge_note" "conventions" {
  org_id  = devin_organization.team.org_id
  name    = "API Conventions"
  body    = file("knowledge/api-conventions.md")
  trigger = "When working on API endpoints"
}

resource "devin_secret" "npm_token" {
  org_id = devin_organization.team.org_id
  key    = "NPM_TOKEN"
  value  = var.npm_token
}

resource "devin_schedule" "nightly_triage" {
  org_id    = devin_organization.team.org_id
  name      = "Nightly issue triage"
  prompt    = "Triage new GitHub issues and propose fixes for the easy ones."
  frequency = "0 6 * * *"
}

resource "devin_ip_access_list" "office" {
  ip_ranges = ["203.0.113.0/24"]
}

resource "devin_idp_group" "engineering" {
  name = "engineering"
}

resource "devin_org_idp_group_role" "engineering_member" {
  org_id         = devin_organization.team.org_id
  idp_group_name = devin_idp_group.engineering.name
  role_id        = [for r in data.devin_roles.all.roles : r.role_id if r.role_type == "org"][0]
}
```

## Resources

| Resource                             | Description                                                  |
| ------------------------------------ | ------------------------------------------------------------ |
| `devin_organization`                 | Enterprise organization                                      |
| `devin_git_permission`               | Git repository access for an org                             |
| `devin_playbook`                     | Playbook within an org                                       |
| `devin_knowledge_note`               | Knowledge note within an org                                 |
| `devin_enterprise_playbook`          | Enterprise-level playbook shared across orgs                 |
| `devin_enterprise_knowledge_note`    | Enterprise-level knowledge note shared across orgs           |
| `devin_secret`                       | Org-level secret for Devin sessions                          |
| `devin_schedule`                     | Recurring or one-time session schedule                       |
| `devin_ip_access_list`               | Enterprise IP access list (singleton)                        |
| `devin_org_group_limits`             | Enterprise org-group ACU limits (singleton, feature-flagged) |
| `devin_org_tags`                     | Org allowed session tags + default tag (singleton)           |
| `devin_idp_group`                    | Registers an IdP group name                                  |
| `devin_enterprise_idp_group_role`    | Maps an IdP group to an enterprise-wide role                 |
| `devin_org_idp_group_role`           | Maps an IdP group to an org-scoped role                      |
| `devin_enterprise_service_user_role` | Enterprise role for an existing service user                 |
| `devin_org_service_user_role`        | Org role for an existing service user                        |
| `devin_org_user_role`                | Org role for an existing enterprise user                     |

## Data sources

| Data source             | Description                                      |
| ----------------------- | ------------------------------------------------ |
| `devin_organizations`   | Organizations in the enterprise                  |
| `devin_git_connections` | Git connections available to the enterprise      |
| `devin_roles`           | Roles available for membership and service users |
| `devin_users`           | Users in the enterprise                          |
| `devin_service_users`   | Active service users in the enterprise           |
| `devin_idp_groups`      | IdP groups registered with the enterprise        |

## List resources (Terraform 1.14+)

Resources whose APIs expose list endpoints also support Terraform's `list` blocks (`terraform query`), so existing infrastructure can be discovered and imported by identity: `devin_organization`, `devin_playbook`, `devin_enterprise_playbook`, `devin_knowledge_note`, `devin_enterprise_knowledge_note`, `devin_schedule`, `devin_idp_group`, and `devin_git_permission`. `devin_secret` is also listable for discovery and inventory, but secrets cannot be imported: the API never returns secret values, so an import could not populate the required `value` attribute.

```hcl
# discover.tfquery.hcl
list "devin_playbook" "all" {
  provider = devin

  config {
    org_id = "org-abc123"
  }
}
```

## Advanced examples

### Loading every playbook in a folder

Keep playbook bodies as Markdown files in your repository and let Terraform
manage one `devin_playbook` per file. `fileset` discovers the files and
`for_each` creates a resource for each, so adding or removing a `.md` file is
the only change needed to add or remove a playbook.

```hcl
locals {
  playbook_dir = "${path.module}/playbooks"
}

resource "devin_playbook" "from_folder" {
  for_each = fileset(local.playbook_dir, "*.md")

  org_id = devin_organization.team.org_id
  title  = trimsuffix(each.value, ".md")
  body   = file("${local.playbook_dir}/${each.value}")
}
```

Each resource is keyed by its filename (e.g. `devin_playbook.from_folder["coding-standards.md"]`), so renaming a file replaces only that playbook.

## Authentication

The provider authenticates with a service user credential (`cog_` prefix),
provisioned via the Devin settings UI or the v3 API (see the
[authentication docs](https://docs.devin.ai/api-reference/authentication)).
Provide it through the `DEVIN_TOKEN` environment variable:

```bash
export DEVIN_TOKEN=cog_...
# Optional; defaults to https://api.devin.ai
export DEVIN_API_URL=https://api.devin.ai
```

Or pass it as a `sensitive` variable:

```hcl
variable "devin_token" {
  type      = string
  sensitive = true
}

provider "devin" {
  token = var.devin_token
}
```

For enterprise operations (creating orgs, managing git permissions across orgs, listing git connections and roles), the service user needs enterprise-level permissions. For org-scoped operations (playbooks, knowledge, secrets, schedules), org-level permissions suffice.

## Development

```bash
# Build
make build

# Run unit tests
make test

# Install locally for manual testing
make install

# Regenerate registry documentation (docs/)
make docs
```

Resource schemas and CRUD are hand-written. The API request/response models in `internal/api/models.gen.go` are generated from the Devin v3 OpenAPI spec with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) as part of the provider's release pipeline.

### Documentation

The Terraform Registry documentation under `docs/` is generated with [tfplugindocs](https://github.com/hashicorp/terraform-plugin-docs) from the schema `Description` fields in `internal/provider/` and the configuration examples under `examples/` (`examples/provider/provider.tf` plus `examples/resources/<resource>/resource.tf` and `import.sh`, and `examples/data-sources/<data source>/data-source.tf`). After changing a schema or an example, run `make docs` and commit the result — CI fails if `docs/` is stale. Custom page layouts can be added as templates under `templates/` if the defaults ever stop being enough.

### Acceptance tests

Acceptance tests (`internal/provider/*_acc_test.go`) run real Terraform plans and applies against a live Devin API and are gated behind `TF_ACC=1`:

```bash
export DEVIN_API_URL=...    # must point at a local instance (localhost)
export DEVIN_TOKEN=cog_...  # enterprise service user token
# Optional, enables the org-scoped token tests:
export DEVIN_ORG_TOKEN=cog_...
export DEVIN_ORG_TOKEN_ORG_ID=org-...
# Optional, enables the git permission CRUD tests:
export DEVIN_GIT_CONNECTION_ID=git-connection-...
# Optional, enables the org tags tests (enterprise with required_tags_enabled):
export DEVIN_TAGS_TOKEN=cog_...
# Optional, enables the enterprise service user role tests (service user
# without an account-level role):
export DEVIN_ROLE_SU_ID=su-...
TF_ACC=1 go test ./... -v -timeout 30m
```

The tests create and delete organizations, git permissions, playbooks, knowledge notes, secrets, and schedules; the precheck refuses to run against anything other than a localhost API.
