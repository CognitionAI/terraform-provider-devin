package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Resource identity schemas and models. Identities make the resources
// importable by identity and listable via Terraform's `list` blocks (see
// list_resources.go).

func identityAttribute(description string) identityschema.StringAttribute {
	return identityschema.StringAttribute{
		RequiredForImport: true,
		Description:       description,
	}
}

// setIdentity writes the identity value when Terraform requested identity
// data (resp.Identity is nil on Terraform versions without identity support).
func setIdentity(ctx context.Context, identity *tfsdk.ResourceIdentity, value any, diags *diag.Diagnostics) {
	if identity == nil {
		return
	}
	diags.Append(identity.Set(ctx, value)...)
}

// importComposite imports a resource keyed by multiple attributes, reading
// them from the import identity when present or from a "/"-separated ID
// string otherwise, and writes them to both state and the returned identity.
func importComposite(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse, attrNames ...string) {
	values := make([]types.String, len(attrNames))
	if req.ID == "" && req.Identity != nil {
		for i, name := range attrNames {
			resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root(name), &values[i])...)
			if resp.Diagnostics.HasError() {
				return
			}
		}
	} else {
		parts := strings.Split(req.ID, "/")
		valid := len(parts) == len(attrNames)
		for _, part := range parts {
			if part == "" {
				valid = false
			}
		}
		if !valid {
			resp.Diagnostics.AddError(
				"Unexpected import identifier",
				fmt.Sprintf("Expected import identifier of the form %q, got: %q", strings.Join(attrNames, "/"), req.ID),
			)
			return
		}
		for i, part := range parts {
			values[i] = types.StringValue(part)
		}
	}
	for i, name := range attrNames {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), values[i])...)
		if resp.Identity != nil {
			resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root(name), values[i])...)
		}
	}
}

type organizationIdentityModel struct {
	OrgID types.String `tfsdk:"org_id"`
}

func organizationIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_id": identityAttribute("Organization ID."),
		},
	}
}

type playbookIdentityModel struct {
	OrgID      types.String `tfsdk:"org_id"`
	PlaybookID types.String `tfsdk:"playbook_id"`
}

func playbookIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_id":      identityAttribute("Organization ID that owns the playbook."),
			"playbook_id": identityAttribute("Playbook ID."),
		},
	}
}

type enterprisePlaybookIdentityModel struct {
	PlaybookID types.String `tfsdk:"playbook_id"`
}

func enterprisePlaybookIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"playbook_id": identityAttribute("Playbook ID."),
		},
	}
}

type knowledgeNoteIdentityModel struct {
	OrgID  types.String `tfsdk:"org_id"`
	NoteID types.String `tfsdk:"note_id"`
}

func knowledgeNoteIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_id":  identityAttribute("Organization ID that owns the note."),
			"note_id": identityAttribute("Knowledge note ID."),
		},
	}
}

type enterpriseKnowledgeNoteIdentityModel struct {
	NoteID types.String `tfsdk:"note_id"`
}

func enterpriseKnowledgeNoteIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"note_id": identityAttribute("Knowledge note ID."),
		},
	}
}

type secretIdentityModel struct {
	OrgID    types.String `tfsdk:"org_id"`
	SecretID types.String `tfsdk:"secret_id"`
}

func secretIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_id":    identityAttribute("Organization ID that owns the secret."),
			"secret_id": identityAttribute("Secret ID."),
		},
	}
}

type scheduleIdentityModel struct {
	OrgID      types.String `tfsdk:"org_id"`
	ScheduleID types.String `tfsdk:"schedule_id"`
}

func scheduleIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_id":      identityAttribute("Organization ID that owns the schedule."),
			"schedule_id": identityAttribute("Schedule ID."),
		},
	}
}

type idpGroupIdentityModel struct {
	Name types.String `tfsdk:"name"`
}

func idpGroupIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"name": identityAttribute("Name of the IdP group."),
		},
	}
}

type gitPermissionIdentityModel struct {
	OrgID           types.String `tfsdk:"org_id"`
	GitPermissionID types.String `tfsdk:"git_permission_id"`
}

func gitPermissionIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_id":            identityAttribute("Organization ID the permission applies to."),
			"git_permission_id": identityAttribute("Git permission ID."),
		},
	}
}
