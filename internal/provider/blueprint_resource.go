package provider

import (
	"context"
	"net/http"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &blueprintResource{}
var _ resource.ResourceWithImportState = &blueprintResource{}
var _ resource.ResourceWithIdentity = &blueprintResource{}

type blueprintResource struct {
	client *Client
}

type blueprintModel struct {
	BlueprintID types.String `tfsdk:"blueprint_id"`
	OrgID       types.String `tfsdk:"org_id"`
	Contents    types.String `tfsdk:"contents"`
	RepoName    types.String `tfsdk:"repo_name"`
	Type        types.String `tfsdk:"type"`
	CreatedAt   types.Int64  `tfsdk:"created_at"`
	UpdatedAt   types.Int64  `tfsdk:"updated_at"`
}

func NewBlueprintResource() resource.Resource {
	return &blueprintResource{}
}

func (r *blueprintResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint"
}

func (r *blueprintResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Devin environment blueprint (org- or repo-tier) via the snapshot-setup API.",
		Attributes: map[string]schema.Attribute{
			"blueprint_id": schema.StringAttribute{
				Description: "Blueprint ID assigned by Devin.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns this blueprint.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"contents": schema.StringAttribute{
				Description: "Blueprint YAML contents. Stored in Devin and fetched via presigned URL on read.",
				Required:    true,
			},
			"repo_name": schema.StringAttribute{
				Description: "Repository name (e.g. 'myorg/myrepo') for repo-tier blueprints. Omit for org-tier.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Description: "Blueprint tier: org or repo (assigned by Devin).",
				Computed:    true,
			},
			"created_at": schema.Int64Attribute{
				Description: "Unix timestamp when the blueprint was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.Int64Attribute{
				Description: "Unix timestamp when the blueprint was last updated.",
				Computed:    true,
			},
		},
	}
}

func (r *blueprintResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = blueprintIdentitySchema()
}

func (r *blueprintResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *blueprintResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan blueprintModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.BlueprintCreateRequest{
		Contents: stringPtrFrom(plan.Contents),
		RepoName: stringPtrFrom(plan.RepoName),
	}

	var result api.BlueprintResponse
	if err := r.client.Post(ctx, orgBlueprintsPath(plan.OrgID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to create blueprint", err.Error())
		return
	}

	contents, err := fetchBlueprintContents(ctx, r.client, plan.OrgID.ValueString(), result.BlueprintID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read blueprint contents after create", err.Error())
		return
	}

	mapBlueprintResponseToModel(&result, contents, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, blueprintIdentityModel{OrgID: plan.OrgID, BlueprintID: plan.BlueprintID}, &resp.Diagnostics)
}

func (r *blueprintResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state blueprintModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.BlueprintResponse
	err := r.client.Get(ctx, orgBlueprintPath(state.OrgID.ValueString(), state.BlueprintID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, blueprintIdentityModel{OrgID: state.OrgID, BlueprintID: state.BlueprintID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read blueprint", err.Error())
		return
	}

	contents, err := fetchBlueprintContents(ctx, r.client, state.OrgID.ValueString(), state.BlueprintID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch blueprint contents", err.Error())
		return
	}

	mapBlueprintResponseToModel(&result, contents, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, blueprintIdentityModel{OrgID: state.OrgID, BlueprintID: state.BlueprintID}, &resp.Diagnostics)
}

func (r *blueprintResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan blueprintModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.BlueprintUpdateRequest{
		Contents: stringPtrFrom(plan.Contents),
	}

	var result api.BlueprintResponse
	err := r.client.do(ctx, http.MethodPatch, orgBlueprintPath(plan.OrgID.ValueString(), plan.BlueprintID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update blueprint", err.Error())
		return
	}

	contents, err := fetchBlueprintContents(ctx, r.client, plan.OrgID.ValueString(), plan.BlueprintID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch blueprint contents after update", err.Error())
		return
	}

	mapBlueprintResponseToModel(&result, contents, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, blueprintIdentityModel{OrgID: plan.OrgID, BlueprintID: plan.BlueprintID}, &resp.Diagnostics)
}

func (r *blueprintResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state blueprintModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, orgBlueprintPath(state.OrgID.ValueString(), state.BlueprintID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete blueprint", err.Error())
	}
}

func (r *blueprintResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "blueprint_id")
}
