package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &enterpriseKnowledgeNoteResource{}
var _ resource.ResourceWithImportState = &enterpriseKnowledgeNoteResource{}
var _ resource.ResourceWithIdentity = &enterpriseKnowledgeNoteResource{}

type enterpriseKnowledgeNoteResource struct {
	client *Client
}

type enterpriseKnowledgeNoteModel struct {
	NoteID     types.String `tfsdk:"note_id"`
	Name       types.String `tfsdk:"name"`
	Body       types.String `tfsdk:"body"`
	Trigger    types.String `tfsdk:"trigger"`
	PinnedRepo types.String `tfsdk:"pinned_repo"`
	IsEnabled  types.Bool   `tfsdk:"is_enabled"`
}

func NewEnterpriseKnowledgeNoteResource() resource.Resource {
	return &enterpriseKnowledgeNoteResource{}
}

func (r *enterpriseKnowledgeNoteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_knowledge_note"
}

func (r *enterpriseKnowledgeNoteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an enterprise-level Devin knowledge note, shared across all organizations in the enterprise. " +
			"Use devin_knowledge_note for organization-scoped notes.",
		Attributes: map[string]schema.Attribute{
			"note_id": schema.StringAttribute{
				Description: "Knowledge note ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name/title of the knowledge note.",
				Required:    true,
			},
			"body": schema.StringAttribute{
				Description: "Body content of the knowledge note (markdown).",
				Required:    true,
			},
			"trigger": schema.StringAttribute{
				Description: "When this knowledge note is retrieved. Describes the trigger/scope condition.",
				Required:    true,
			},
			"pinned_repo": schema.StringAttribute{
				Description: "Repository this note is pinned to (e.g., 'myorg/myrepo').",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"is_enabled": schema.BoolAttribute{
				Description: "Whether the note is enabled. Defaults to true.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *enterpriseKnowledgeNoteResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = enterpriseKnowledgeNoteIdentitySchema()
}

func (r *enterpriseKnowledgeNoteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *enterpriseKnowledgeNoteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterpriseKnowledgeNoteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.KnowledgeNoteCreateRequest{
		Name:       plan.Name.ValueString(),
		Body:       plan.Body.ValueString(),
		Trigger:    plan.Trigger.ValueString(),
		PinnedRepo: optionalStringFrom(plan.PinnedRepo),
	}
	if !plan.IsEnabled.IsNull() && !plan.IsEnabled.IsUnknown() {
		body.IsEnabled = nullable.NewNullableWithValue(plan.IsEnabled.ValueBool())
	}

	var result api.KnowledgeNoteResponse
	err := r.client.Post(ctx, enterpriseKnowledgeNotesPath, body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create enterprise knowledge note", err.Error())
		return
	}

	mapEnterpriseKnowledgeNoteResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, enterpriseKnowledgeNoteIdentityModel{NoteID: plan.NoteID}, &resp.Diagnostics)
}

func (r *enterpriseKnowledgeNoteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterpriseKnowledgeNoteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.KnowledgeNoteResponse
	err := r.client.Get(ctx, enterpriseKnowledgeNotePath(state.NoteID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, enterpriseKnowledgeNoteIdentityModel{NoteID: state.NoteID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read enterprise knowledge note", err.Error())
		return
	}

	if result.AccessType != api.KnowledgeNoteResponseAccessTypeEnterprise {
		resp.Diagnostics.AddError(
			"Not an enterprise knowledge note",
			"The note exists but is org-scoped, not enterprise-scoped. Use devin_knowledge_note instead.",
		)
		return
	}

	mapEnterpriseKnowledgeNoteResponseToModel(&result, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, enterpriseKnowledgeNoteIdentityModel{NoteID: state.NoteID}, &resp.Diagnostics)
}

func (r *enterpriseKnowledgeNoteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan enterpriseKnowledgeNoteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API rebuilds the note's trigger metadata from the request, so
	// pinned_repo is always sent; null clears the pin.
	body := api.KnowledgeNoteCreateRequest{
		Name:       plan.Name.ValueString(),
		Body:       plan.Body.ValueString(),
		Trigger:    plan.Trigger.ValueString(),
		PinnedRepo: nullableStringFrom(plan.PinnedRepo),
	}
	if !plan.IsEnabled.IsNull() && !plan.IsEnabled.IsUnknown() {
		body.IsEnabled = nullable.NewNullableWithValue(plan.IsEnabled.ValueBool())
	}

	var result api.KnowledgeNoteResponse
	err := r.client.Put(ctx, enterpriseKnowledgeNotePath(plan.NoteID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update enterprise knowledge note", err.Error())
		return
	}

	mapEnterpriseKnowledgeNoteResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, enterpriseKnowledgeNoteIdentityModel{NoteID: plan.NoteID}, &resp.Diagnostics)
}

func (r *enterpriseKnowledgeNoteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enterpriseKnowledgeNoteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, enterpriseKnowledgeNotePath(state.NoteID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete enterprise knowledge note", err.Error())
	}
}

func (r *enterpriseKnowledgeNoteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("note_id"), path.Root("note_id"), req, resp)
}

func mapEnterpriseKnowledgeNoteResponseToModel(resp *api.KnowledgeNoteResponse, model *enterpriseKnowledgeNoteModel) {
	model.NoteID = types.StringValue(resp.NoteID)
	model.Name = types.StringValue(resp.Name)
	model.Body = types.StringValue(resp.Body)
	model.Trigger = types.StringValue(resp.Trigger)
	model.PinnedRepo = stringFromNullable(resp.PinnedRepo)
	model.IsEnabled = types.BoolValue(resp.IsEnabled)
}
