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

var _ resource.Resource = &orgServiceUserRoleResource{}
var _ resource.ResourceWithImportState = &orgServiceUserRoleResource{}

type orgServiceUserRoleResource struct {
	client *Client
}

type orgServiceUserRoleModel struct {
	OrgID         types.String `tfsdk:"org_id"`
	ServiceUserID types.String `tfsdk:"service_user_id"`
	RoleID        types.String `tfsdk:"role_id"`
}

func NewOrgServiceUserRoleResource() resource.Resource {
	return &orgServiceUserRoleResource{}
}

func (r *orgServiceUserRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_service_user_role"
}

func (r *orgServiceUserRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Assigns a role scoped to a single organization to an existing service user. The service user " +
			"must not already have a role in that organization. Use devin_enterprise_service_user_role for " +
			"enterprise-wide roles.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID the role assignment is scoped to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"service_user_id": schema.StringAttribute{
				Description: "ID of an existing service user.",
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

func (r *orgServiceUserRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *orgServiceUserRoleResource) assignmentPath(model orgServiceUserRoleModel) string {
	return orgMemberServiceUserPath(model.OrgID.ValueString(), model.ServiceUserID.ValueString())
}

func (r *orgServiceUserRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgServiceUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.ServiceUserUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.ServiceUser
	if err := r.client.Post(ctx, r.assignmentPath(plan), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to assign org service user role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgServiceUserRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgServiceUserRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is no org-scoped GET for a single service user; the enterprise
	// GET returns all of its role assignments.
	var result api.ServiceUser
	err := r.client.Get(ctx, enterpriseMemberServiceUserPath(state.ServiceUserID.ValueString()), &result)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org service user role", err.Error())
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

func (r *orgServiceUserRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgServiceUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.ServiceUserUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.ServiceUser
	if err := r.client.Patch(ctx, r.assignmentPath(plan), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update org service user role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgServiceUserRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgServiceUserRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, r.assignmentPath(state), nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete org service user role", err.Error())
	}
}

func (r *orgServiceUserRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "service_user_id")
}
