package provider

import (
	"context"
	"net/http"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &playbookResource{}
var _ resource.ResourceWithImportState = &playbookResource{}
var _ resource.ResourceWithIdentity = &playbookResource{}

type playbookResource struct {
	client *Client
}

type playbookModel struct {
	PlaybookID types.String `tfsdk:"playbook_id"`
	OrgID      types.String `tfsdk:"org_id"`
	Title      types.String `tfsdk:"title"`
	Body       types.String `tfsdk:"body"`
	Macro      types.String `tfsdk:"macro"`
}

func NewPlaybookResource() resource.Resource {
	return &playbookResource{}
}

func (r *playbookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_playbook"
}

func (r *playbookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Devin playbook within an organization.",
		Attributes: map[string]schema.Attribute{
			"playbook_id": schema.StringAttribute{
				Description: "Playbook ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns this playbook.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
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

func (r *playbookResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = playbookIdentitySchema()
}

func (r *playbookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *playbookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan playbookModel
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
	err := r.client.Post(ctx, orgPlaybooksPath(plan.OrgID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create playbook", err.Error())
		return
	}

	mapPlaybookResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, playbookIdentityModel{OrgID: plan.OrgID, PlaybookID: plan.PlaybookID}, &resp.Diagnostics)
}

func (r *playbookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state playbookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.PlaybookResponse
	err := r.client.Get(ctx, orgPlaybookPath(state.OrgID.ValueString(), state.PlaybookID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, playbookIdentityModel{OrgID: state.OrgID, PlaybookID: state.PlaybookID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read playbook", err.Error())
		return
	}

	mapPlaybookResponseToModel(&result, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, playbookIdentityModel{OrgID: state.OrgID, PlaybookID: state.PlaybookID}, &resp.Diagnostics)
}

func (r *playbookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan playbookModel
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
	err := r.client.do(ctx, http.MethodPut, orgPlaybookPath(plan.OrgID.ValueString(), plan.PlaybookID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update playbook", err.Error())
		return
	}

	mapPlaybookResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, playbookIdentityModel{OrgID: plan.OrgID, PlaybookID: plan.PlaybookID}, &resp.Diagnostics)
}

func (r *playbookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state playbookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, orgPlaybookPath(state.OrgID.ValueString(), state.PlaybookID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete playbook", err.Error())
	}
}

func (r *playbookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "playbook_id")
}

func mapPlaybookResponseToModel(resp *api.PlaybookResponse, model *playbookModel) {
	model.PlaybookID = types.StringValue(resp.PlaybookID)
	model.Title = types.StringValue(resp.Title)
	model.Body = types.StringValue(resp.Body)
	model.Macro = stringFromNullable(resp.Macro)
	if orgID := stringFromNullable(resp.OrgID); !orgID.IsNull() {
		model.OrgID = orgID
	}
}
