package provider

import (
	"context"
	"net/http"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &knowledgeNoteResource{}
var _ resource.ResourceWithImportState = &knowledgeNoteResource{}
var _ resource.ResourceWithIdentity = &knowledgeNoteResource{}

type knowledgeNoteResource struct {
	client *Client
}

type knowledgeNoteModel struct {
	NoteID     types.String `tfsdk:"note_id"`
	OrgID      types.String `tfsdk:"org_id"`
	Name       types.String `tfsdk:"name"`
	Body       types.String `tfsdk:"body"`
	Trigger    types.String `tfsdk:"trigger"`
	PinnedRepo types.String `tfsdk:"pinned_repo"`
	FolderID   types.String `tfsdk:"folder_id"`
	IsEnabled  types.Bool   `tfsdk:"is_enabled"`
}

func NewKnowledgeNoteResource() resource.Resource {
	return &knowledgeNoteResource{}
}

func (r *knowledgeNoteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_knowledge_note"
}

func (r *knowledgeNoteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Devin knowledge note within an organization.",
		Attributes: map[string]schema.Attribute{
			"note_id": schema.StringAttribute{
				Description: "Knowledge note ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns this knowledge note.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
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
			"folder_id": schema.StringAttribute{
				Description: "ID of the knowledge folder containing this note. Omit for the root folder.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"is_enabled": schema.BoolAttribute{
				Description: "Whether the note is enabled. Defaults to true; a disabled parent folder can override this.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *knowledgeNoteResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = knowledgeNoteIdentitySchema()
}

func (r *knowledgeNoteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *knowledgeNoteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan knowledgeNoteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.KnowledgeNoteCreateRequest{
		Name:       plan.Name.ValueString(),
		Body:       plan.Body.ValueString(),
		Trigger:    plan.Trigger.ValueString(),
		PinnedRepo: optionalStringFrom(plan.PinnedRepo),
		FolderID:   optionalStringFrom(plan.FolderID),
	}
	if !plan.IsEnabled.IsNull() && !plan.IsEnabled.IsUnknown() {
		body.IsEnabled = nullable.NewNullableWithValue(plan.IsEnabled.ValueBool())
	}

	var result api.KnowledgeNoteResponse
	err := r.client.Post(ctx, orgKnowledgeNotesPath(plan.OrgID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create knowledge note", err.Error())
		return
	}

	mapKnowledgeNoteResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, knowledgeNoteIdentityModel{OrgID: plan.OrgID, NoteID: plan.NoteID}, &resp.Diagnostics)
}

func (r *knowledgeNoteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state knowledgeNoteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.KnowledgeNoteResponse
	err := r.client.Get(ctx, orgKnowledgeNotePath(state.OrgID.ValueString(), state.NoteID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, knowledgeNoteIdentityModel{OrgID: state.OrgID, NoteID: state.NoteID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read knowledge note", err.Error())
		return
	}

	mapKnowledgeNoteResponseToModel(&result, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, knowledgeNoteIdentityModel{OrgID: state.OrgID, NoteID: state.NoteID}, &resp.Diagnostics)
}

func (r *knowledgeNoteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan knowledgeNoteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API rebuilds the note's trigger metadata and folder placement from
	// the request, so both fields are always sent; null clears the pin or
	// moves the note back to the root folder.
	body := api.KnowledgeNoteCreateRequest{
		Name:       plan.Name.ValueString(),
		Body:       plan.Body.ValueString(),
		Trigger:    plan.Trigger.ValueString(),
		PinnedRepo: nullableStringFrom(plan.PinnedRepo),
		FolderID:   nullableStringFrom(plan.FolderID),
	}
	if !plan.IsEnabled.IsNull() && !plan.IsEnabled.IsUnknown() {
		body.IsEnabled = nullable.NewNullableWithValue(plan.IsEnabled.ValueBool())
	}

	var result api.KnowledgeNoteResponse
	err := r.client.do(ctx, http.MethodPut, orgKnowledgeNotePath(plan.OrgID.ValueString(), plan.NoteID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update knowledge note", err.Error())
		return
	}

	mapKnowledgeNoteResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, knowledgeNoteIdentityModel{OrgID: plan.OrgID, NoteID: plan.NoteID}, &resp.Diagnostics)
}

func (r *knowledgeNoteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state knowledgeNoteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, orgKnowledgeNotePath(state.OrgID.ValueString(), state.NoteID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete knowledge note", err.Error())
	}
}

func (r *knowledgeNoteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "note_id")
}

func mapKnowledgeNoteResponseToModel(resp *api.KnowledgeNoteResponse, model *knowledgeNoteModel) {
	model.NoteID = types.StringValue(resp.NoteID)
	model.Name = types.StringValue(resp.Name)
	model.Body = types.StringValue(resp.Body)
	model.Trigger = types.StringValue(resp.Trigger)
	model.PinnedRepo = stringFromNullable(resp.PinnedRepo)
	model.FolderID = stringFromNullable(resp.FolderID)
	model.IsEnabled = types.BoolValue(resp.IsEnabled)
	if orgID := stringFromNullable(resp.OrgID); !orgID.IsNull() {
		model.OrgID = orgID
	}
}
