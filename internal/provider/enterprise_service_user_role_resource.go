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

var _ resource.Resource = &enterpriseServiceUserRoleResource{}
var _ resource.ResourceWithImportState = &enterpriseServiceUserRoleResource{}

type enterpriseServiceUserRoleResource struct {
	client *Client
}

type enterpriseServiceUserRoleModel struct {
	ServiceUserID types.String `tfsdk:"service_user_id"`
	RoleID        types.String `tfsdk:"role_id"`
}

func NewEnterpriseServiceUserRoleResource() resource.Resource {
	return &enterpriseServiceUserRoleResource{}
}

func (r *enterpriseServiceUserRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_service_user_role"
}

func (r *enterpriseServiceUserRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Assigns an enterprise-wide (account-level) role to an existing service user. The service user " +
			"must not already have an account-level role. Use devin_org_service_user_role for organization-scoped roles. " +
			"Note: the API has no way to remove an account-level role without deleting the service user itself, so " +
			"destroying this resource only removes it from Terraform state and leaves the assignment in place.",
		Attributes: map[string]schema.Attribute{
			"service_user_id": schema.StringAttribute{
				Description: "ID of an existing service user.",
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

func (r *enterpriseServiceUserRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *enterpriseServiceUserRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterpriseServiceUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.ServiceUserUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.ServiceUser
	if err := r.client.Post(ctx, enterpriseMemberServiceUserPath(plan.ServiceUserID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to assign enterprise service user role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *enterpriseServiceUserRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterpriseServiceUserRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.ServiceUser
	err := r.client.Get(ctx, enterpriseMemberServiceUserPath(state.ServiceUserID.ValueString()), &result)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read enterprise service user role", err.Error())
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

func (r *enterpriseServiceUserRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan enterpriseServiceUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.ServiceUserUpdateRoleRequest{RoleID: plan.RoleID.ValueString()}
	var result api.ServiceUser
	if err := r.client.Patch(ctx, enterpriseMemberServiceUserPath(plan.ServiceUserID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update enterprise service user role", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *enterpriseServiceUserRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// DELETE /v3/enterprise/members/service-users/{id} deletes the service
	// user itself, not just its role assignment, and a service user cannot
	// exist without an account-level role. Removing the resource from state
	// is the only safe destroy behavior.
	resp.Diagnostics.AddWarning(
		"Enterprise service user role left in place",
		"The Devin API cannot remove a service user's enterprise (account-level) role without deleting the "+
			"service user itself, so the role assignment was left in place and only removed from Terraform state. "+
			"Delete the service user to fully revoke its access.",
	)
}

func (r *enterpriseServiceUserRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("service_user_id"), req, resp)
}
