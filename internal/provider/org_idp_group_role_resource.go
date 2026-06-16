package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &orgIdpGroupRoleResource{}
var _ resource.ResourceWithImportState = &orgIdpGroupRoleResource{}

type orgIdpGroupRoleResource struct {
	client *Client
}

type orgIdpGroupRoleModel struct {
	OrgID        types.String `tfsdk:"org_id"`
	IdpGroupName types.String `tfsdk:"idp_group_name"`
	RoleID       types.String `tfsdk:"role_id"`
}

func NewOrgIdpGroupRoleResource() resource.Resource {
	return &orgIdpGroupRoleResource{}
}

func (r *orgIdpGroupRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_idp_group_role"
}

func (r *orgIdpGroupRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Maps a registered IdP group to a role scoped to a single organization. Members of the IdP group " +
			"inherit the assigned role within that organization. Use devin_enterprise_idp_group_role for enterprise-wide roles.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID the role assignment is scoped to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"idp_group_name": schema.StringAttribute{
				Description: "Name of a registered IdP group (see devin_idp_group).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role_id": schema.StringAttribute{
				Description: "Role to assign. Must be an org-level role.",
				Required:    true,
			},
		},
	}
}

func (r *orgIdpGroupRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *orgIdpGroupRoleResource) assignmentPath(model orgIdpGroupRoleModel) string {
	return orgMemberIdpGroupPath(model.OrgID.ValueString(), model.IdpGroupName.ValueString())
}

func (r *orgIdpGroupRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgIdpGroupRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.IdpGroupUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.IdpGroup
	if err := r.client.Post(ctx, r.assignmentPath(plan), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to assign org IdP group role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgIdpGroupRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgIdpGroupRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.IdpGroup
	err := r.client.Get(ctx, r.assignmentPath(state), &result)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org IdP group role", err.Error())
		return
	}

	roleID, found := roleForScope(result.RoleAssignments, state.OrgID)
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	state.RoleID = types.StringValue(roleID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgIdpGroupRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgIdpGroupRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.IdpGroupUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.IdpGroup
	if err := r.client.Patch(ctx, r.assignmentPath(plan), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update org IdP group role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgIdpGroupRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgIdpGroupRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, r.assignmentPath(state), nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete org IdP group role", err.Error())
	}
}

func (r *orgIdpGroupRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Unexpected import identifier", fmt.Sprintf("Expected 'org_id/idp_group_name', got: %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("org_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("idp_group_name"), parts[1])...)
}
