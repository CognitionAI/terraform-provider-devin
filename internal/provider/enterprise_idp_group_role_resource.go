package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &enterpriseIdpGroupRoleResource{}
var _ resource.ResourceWithImportState = &enterpriseIdpGroupRoleResource{}

type enterpriseIdpGroupRoleResource struct {
	client *Client
}

type enterpriseIdpGroupRoleModel struct {
	IdpGroupName types.String `tfsdk:"idp_group_name"`
	RoleID       types.String `tfsdk:"role_id"`
}

func NewEnterpriseIdpGroupRoleResource() resource.Resource {
	return &enterpriseIdpGroupRoleResource{}
}

func (r *enterpriseIdpGroupRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_idp_group_role"
}

func (r *enterpriseIdpGroupRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Maps a registered IdP group to an enterprise-wide (account-level) role. Members of the IdP group " +
			"inherit the assigned role across the enterprise. Use devin_org_idp_group_role for organization-scoped roles.",
		Attributes: map[string]schema.Attribute{
			"idp_group_name": schema.StringAttribute{
				Description: "Name of a registered IdP group (see devin_idp_group).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role_id": schema.StringAttribute{
				Description: "Role to assign. Must be an enterprise (account-level) role.",
				Required:    true,
			},
		},
	}
}

func (r *enterpriseIdpGroupRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *enterpriseIdpGroupRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterpriseIdpGroupRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.IdpGroupUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.IdpGroup
	if err := r.client.Post(ctx, enterpriseMemberIdpGroupPath(plan.IdpGroupName.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to assign enterprise IdP group role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *enterpriseIdpGroupRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterpriseIdpGroupRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.IdpGroup
	err := r.client.Get(ctx, enterpriseMemberIdpGroupPath(state.IdpGroupName.ValueString()), &result)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read enterprise IdP group role", err.Error())
		return
	}

	roleID, found := roleForScope(result.RoleAssignments, types.StringNull())
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	state.RoleID = types.StringValue(roleID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *enterpriseIdpGroupRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan enterpriseIdpGroupRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.IdpGroupUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.IdpGroup
	if err := r.client.Patch(ctx, enterpriseMemberIdpGroupPath(plan.IdpGroupName.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update enterprise IdP group role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *enterpriseIdpGroupRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enterpriseIdpGroupRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, enterpriseMemberIdpGroupPath(state.IdpGroupName.ValueString()), nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete enterprise IdP group role", err.Error())
	}
}

func (r *enterpriseIdpGroupRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("idp_group_name"), req, resp)
}

// roleForScope returns the role_id assigned to the principal (IdP group,
// user, or service user) at the requested scope: the enterprise (account)
// assignment when orgID is null, otherwise the assignment matching that org.
func roleForScope(assignments []api.RoleAssignment, orgID types.String) (string, bool) {
	for _, assignment := range assignments {
		assignedOrg := stringFromNullable(assignment.OrgID)
		if orgID.IsNull() {
			if assignedOrg.IsNull() {
				return assignment.Role.RoleID, true
			}
			continue
		}
		if !assignedOrg.IsNull() && assignedOrg.ValueString() == orgID.ValueString() {
			return assignment.Role.RoleID, true
		}
	}
	return "", false
}
