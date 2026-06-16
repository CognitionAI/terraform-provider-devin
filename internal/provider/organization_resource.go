package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &organizationResource{}
var _ resource.ResourceWithImportState = &organizationResource{}
var _ resource.ResourceWithIdentity = &organizationResource{}

type organizationResource struct {
	client *Client
}

type organizationModel struct {
	OrgID              types.String `tfsdk:"org_id"`
	Name               types.String `tfsdk:"name"`
	MaxSessionACULimit types.Int64  `tfsdk:"max_session_acu_limit"`
	MaxCycleACULimit   types.Int64  `tfsdk:"max_cycle_acu_limit"`
}

func NewOrganizationResource() resource.Resource {
	return &organizationResource{}
}

func (r *organizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *organizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Devin organization within an enterprise.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the organization.",
				Required:    true,
			},
			"max_session_acu_limit": schema.Int64Attribute{
				Description: "Maximum ACU limit per session. Defaults to a server-side value when omitted.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"max_cycle_acu_limit": schema.Int64Attribute{
				Description: "Maximum ACU limit per cycle.",
				Optional:    true,
			},
		},
	}
}

func (r *organizationResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = organizationIdentitySchema()
}

func (r *organizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *organizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An omitted computed limit is unknown at plan time; leave it out so the
	// server default applies.
	body := api.OrganizationCreateRequest{
		Name:               plan.Name.ValueString(),
		MaxSessionAcuLimit: optionalIntFrom(plan.MaxSessionACULimit),
		MaxCycleAcuLimit:   optionalIntFrom(plan.MaxCycleACULimit),
	}

	var result api.OrganizationResponse
	err := r.client.Post(ctx, enterpriseOrganizationsPath, body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create organization", err.Error())
		return
	}

	plan.OrgID = types.StringValue(result.OrgID)
	mapOrgResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, organizationIdentityModel{OrgID: plan.OrgID}, &resp.Diagnostics)
}

func (r *organizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.OrganizationResponse
	err := r.client.Get(ctx, organizationPath(state.OrgID.ValueString()), &result)
	if IsNotFound(err) {
		// The framework requires identity data even when the resource is gone.
		setIdentity(ctx, resp.Identity, organizationIdentityModel{OrgID: state.OrgID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read organization", err.Error())
		return
	}

	mapOrgResponseToModel(&result, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, organizationIdentityModel{OrgID: state.OrgID}, &resp.Diagnostics)
}

func (r *organizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan organizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The PATCH endpoint uses merge-patch semantics: the cycle limit is only
	// cleared when the field is explicitly null, so always include it.
	body := api.OrganizationUpdateRequest{
		Name:               nullable.NewNullableWithValue(plan.Name.ValueString()),
		MaxSessionAcuLimit: optionalIntFrom(plan.MaxSessionACULimit),
		MaxCycleAcuLimit:   nullable.NewNullNullable[int](),
	}
	if !plan.MaxCycleACULimit.IsNull() && !plan.MaxCycleACULimit.IsUnknown() {
		body.MaxCycleAcuLimit = nullable.NewNullableWithValue(int(plan.MaxCycleACULimit.ValueInt64()))
	}

	var result api.OrganizationResponse
	err := r.client.Patch(ctx, organizationPath(plan.OrgID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update organization", err.Error())
		return
	}

	mapOrgResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, organizationIdentityModel{OrgID: plan.OrgID}, &resp.Diagnostics)
}

func (r *organizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, organizationPath(state.OrgID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete organization", err.Error())
	}
}

func (r *organizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("org_id"), path.Root("org_id"), req, resp)
}

func mapOrgResponseToModel(resp *api.OrganizationResponse, model *organizationModel) {
	model.OrgID = types.StringValue(resp.OrgID)
	model.Name = types.StringValue(resp.Name)
	model.MaxSessionACULimit = int64FromNullable(resp.MaxSessionAcuLimit)
	model.MaxCycleACULimit = int64FromNullable(resp.MaxCycleAcuLimit)
}
