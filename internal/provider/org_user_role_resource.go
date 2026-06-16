package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &orgUserRoleResource{}
var _ resource.ResourceWithImportState = &orgUserRoleResource{}

type orgUserRoleResource struct {
	client *Client
}

type orgUserRoleModel struct {
	OrgID  types.String `tfsdk:"org_id"`
	UserID types.String `tfsdk:"user_id"`
	RoleID types.String `tfsdk:"role_id"`
}

func NewOrgUserRoleResource() resource.Resource {
	return &orgUserRoleResource{}
}

func (r *orgUserRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_user_role"
}

func (r *orgUserRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Assigns an organization role to an existing enterprise user, adding them to the organization. " +
			"The user must already be a member of the enterprise and must not already have a role in that organization. " +
			"Destroying the resource removes the user from the organization.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID the role assignment is scoped to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Description: "ID of an existing enterprise user (see the devin_users data source).",
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

func (r *orgUserRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *orgUserRoleResource) assignmentPath(model orgUserRoleModel) string {
	return orgMemberUserPath(model.OrgID.ValueString(), model.UserID.ValueString())
}

func (r *orgUserRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UserUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.User
	if err := r.client.Post(ctx, r.assignmentPath(plan), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to assign org user role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgUserRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgUserRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is no org-scoped GET for a single user; the enterprise GET
	// returns all of the user's direct role assignments.
	var result api.UserWithIdpRoles
	err := r.client.Get(ctx, enterpriseMemberUserPath(state.UserID.ValueString()), &result)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org user role", err.Error())
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

func (r *orgUserRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UserUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.User
	if err := r.client.Patch(ctx, r.assignmentPath(plan), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update org user role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgUserRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgUserRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, r.assignmentPath(state), nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete org user role", err.Error())
	}
}

func (r *orgUserRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "user_id")
}
