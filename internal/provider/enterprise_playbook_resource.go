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

var _ resource.Resource = &enterprisePlaybookResource{}
var _ resource.ResourceWithImportState = &enterprisePlaybookResource{}
var _ resource.ResourceWithIdentity = &enterprisePlaybookResource{}

type enterprisePlaybookResource struct {
	client *Client
}

type enterprisePlaybookModel struct {
	PlaybookID types.String `tfsdk:"playbook_id"`
	Title      types.String `tfsdk:"title"`
	Body       types.String `tfsdk:"body"`
	Macro      types.String `tfsdk:"macro"`
}

func NewEnterprisePlaybookResource() resource.Resource {
	return &enterprisePlaybookResource{}
}

func (r *enterprisePlaybookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_playbook"
}

func (r *enterprisePlaybookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an enterprise-level Devin playbook, shared across all organizations in the enterprise. " +
			"Use devin_playbook for organization-scoped playbooks.",
		Attributes: map[string]schema.Attribute{
			"playbook_id": schema.StringAttribute{
				Description: "Playbook ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"title": schema.StringAttribute{
				Description: "Playbook title.",
				Required:    true,
			},
			"body": schema.StringAttribute{
				Description: "Playbook body content (markdown).",
				Required:    true,
			},
			"macro": schema.StringAttribute{
				Description: "Playbook macro identifier (e.g., '!my_macro'). Must start with '!' followed by letters, digits, underscores, or hyphens.",
				Optional:    true,
			},
		},
	}
}

func (r *enterprisePlaybookResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = enterprisePlaybookIdentitySchema()
}

func (r *enterprisePlaybookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *enterprisePlaybookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterprisePlaybookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.PlaybookCreateRequest{
		Title: plan.Title.ValueString(),
		Body:  plan.Body.ValueString(),
		Macro: optionalStringFrom(plan.Macro),
	}

	var result api.PlaybookResponse
	err := r.client.Post(ctx, enterprisePlaybooksPath, body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create enterprise playbook", err.Error())
		return
	}

	mapEnterprisePlaybookResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, enterprisePlaybookIdentityModel{PlaybookID: plan.PlaybookID}, &resp.Diagnostics)
}

func (r *enterprisePlaybookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterprisePlaybookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.PlaybookResponse
	err := r.client.Get(ctx, enterprisePlaybookPath(state.PlaybookID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, enterprisePlaybookIdentityModel{PlaybookID: state.PlaybookID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read enterprise playbook", err.Error())
		return
	}

	if result.AccessType != api.PlaybookResponseAccessTypeEnterprise {
		resp.Diagnostics.AddError(
			"Not an enterprise playbook",
			"The playbook exists but is org-scoped, not enterprise-scoped. Use devin_playbook instead.",
		)
		return
	}

	mapEnterprisePlaybookResponseToModel(&result, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, enterprisePlaybookIdentityModel{PlaybookID: state.PlaybookID}, &resp.Diagnostics)
}

func (r *enterprisePlaybookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan enterprisePlaybookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API only clears the macro when the field is explicitly null; omitting
	// it leaves the existing macro unchanged.
	body := api.PlaybookCreateRequest{
		Title: plan.Title.ValueString(),
		Body:  plan.Body.ValueString(),
		Macro: nullableStringFrom(plan.Macro),
	}

	var result api.PlaybookResponse
	err := r.client.Put(ctx, enterprisePlaybookPath(plan.PlaybookID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update enterprise playbook", err.Error())
		return
	}

	mapEnterprisePlaybookResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, enterprisePlaybookIdentityModel{PlaybookID: plan.PlaybookID}, &resp.Diagnostics)
}

func (r *enterprisePlaybookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enterprisePlaybookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, enterprisePlaybookPath(state.PlaybookID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete enterprise playbook", err.Error())
	}
}

func (r *enterprisePlaybookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("playbook_id"), path.Root("playbook_id"), req, resp)
}

func mapEnterprisePlaybookResponseToModel(resp *api.PlaybookResponse, model *enterprisePlaybookModel) {
	model.PlaybookID = types.StringValue(resp.PlaybookID)
	model.Title = types.StringValue(resp.Title)
	model.Body = types.StringValue(resp.Body)
	model.Macro = stringFromNullable(resp.Macro)
}
