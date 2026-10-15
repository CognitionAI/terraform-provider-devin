package provider

import (
	"context"
	"strings"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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

var _ resource.Resource = &blueprintResource{}
var _ resource.ResourceWithImportState = &blueprintResource{}
var _ resource.ResourceWithIdentity = &blueprintResource{}

type blueprintResource struct {
	client *Client
}

type blueprintModel struct {
	BlueprintID                types.String `tfsdk:"blueprint_id"`
	OrgID                      types.String `tfsdk:"org_id"`
	RepoName                   types.String `tfsdk:"repo_name"`
	Type                       types.String `tfsdk:"type"`
	Contents                   types.String `tfsdk:"contents"`
	Source                     types.String `tfsdk:"source"`
	BuildOnDefaultBranchCommit types.Bool   `tfsdk:"build_on_default_branch_commit"`
	ReportCheckRuns            types.Bool   `tfsdk:"report_check_runs"`
	Position                   types.Int64  `tfsdk:"position"`
}

func NewBlueprintResource() resource.Resource {
	return &blueprintResource{}
}

func (r *blueprintResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint"
}

func (r *blueprintResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an organization's snapshot setup blueprint: the org-wide blueprint when repo_name is " +
			"omitted, or a repository's blueprint when it is set. Changes don't start a build; Devin builds on its " +
			"own schedule or when a build is triggered.",
		Attributes: map[string]schema.Attribute{
			"blueprint_id": schema.StringAttribute{
				Description: "Blueprint ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns the blueprint.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"repo_name": schema.StringAttribute{
				Description: "Repository the blueprint is for, e.g. owner/repo. Omit for the org-wide blueprint.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Description: "Blueprint tier: org or repo.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"contents": schema.StringAttribute{
				Description: "Blueprint YAML. Not allowed when source is git, since the YAML then comes from " +
					".devin/blueprint.yaml in the repository. When omitted, Devin's copy isn't managed.",
				Optional: true,
			},
			"source": schema.StringAttribute{
				Description: "Repository blueprints only. Where the YAML is authored: git (synced from " +
					".devin/blueprint.yaml on the default branch) or database (stored in Devin). Defaults to database.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("git", "database"),
				},
			},
			"position": schema.Int64Attribute{
				Description: "Repository blueprints only. Execution position among the organization's repository blueprints. " +
					"Devin doesn't return it, so changes made outside Terraform aren't detected.",
				Optional: true,
			},
			"build_on_default_branch_commit": schema.BoolAttribute{
				Description: "Repository blueprints on github.com only. Whether each push to the default branch builds the environment.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"report_check_runs": schema.BoolAttribute{
				Description: "Repository blueprints on github.com only. Whether each commit build is reported as a GitHub " +
					"check run. Requires build_on_default_branch_commit.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
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

	body := api.CreateOrgBlueprintBody{
		RepoName: optionalStringFrom(plan.RepoName),
		Contents: optionalStringFrom(plan.Contents),
	}
	if !plan.Source.IsNull() && !plan.Source.IsUnknown() {
		body.Source = nullable.NewNullableWithValue(api.CreateOrgBlueprintBodySource(plan.Source.ValueString()))
	}

	var result api.Blueprint
	if err := r.client.Post(ctx, orgBlueprintsPath(plan.OrgID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to create blueprint", err.Error())
		return
	}
	plan.BlueprintID = types.StringValue(result.BlueprintID)
	// Save the ID first so a failed settings update doesn't orphan the blueprint.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("blueprint_id"), plan.BlueprintID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("org_id"), plan.OrgID)...)

	// Position and commit-build settings are only accepted on update.
	update := api.UpdateOrgBlueprintBody{
		BuildOnDefaultBranchCommit: optionalBoolFrom(plan.BuildOnDefaultBranchCommit),
		ReportCheckRuns:            optionalBoolFrom(plan.ReportCheckRuns),
		Position:                   optionalIntFrom(plan.Position),
	}
	if update.BuildOnDefaultBranchCommit.IsSpecified() || update.ReportCheckRuns.IsSpecified() || update.Position.IsSpecified() {
		if err := r.client.Patch(ctx, orgBlueprintPath(plan.OrgID.ValueString(), result.BlueprintID), update, &result); err != nil {
			resp.Diagnostics.AddError("Failed to update blueprint settings", err.Error())
			return
		}
	}

	mapBlueprintToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, blueprintIdentityModel{OrgID: plan.OrgID, BlueprintID: plan.BlueprintID}, &resp.Diagnostics)
}

func (r *blueprintResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state blueprintModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	identity := blueprintIdentityModel{OrgID: state.OrgID, BlueprintID: state.BlueprintID}

	var result api.Blueprint
	err := r.client.Get(ctx, orgBlueprintPath(state.OrgID.ValueString(), state.BlueprintID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, identity, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read blueprint", err.Error())
		return
	}
	mapBlueprintToModel(&result, &state)

	// Only compare contents Terraform manages; git-sourced YAML comes from the repo.
	if !state.Contents.IsNull() && state.Source.ValueString() != "git" {
		contents, ok := readBlueprintContents(ctx, r.client, orgBlueprintPath(state.OrgID.ValueString(), state.BlueprintID.ValueString()), &resp.Diagnostics)
		if !ok {
			return
		}
		state.Contents = contents
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, identity, &resp.Diagnostics)
}

func (r *blueprintResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state blueprintModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateOrgBlueprintBody{}
	if !plan.Contents.IsNull() && !plan.Contents.Equal(state.Contents) {
		body.Contents = nullable.NewNullableWithValue(plan.Contents.ValueString())
	}
	if !plan.Source.IsNull() && !plan.Source.IsUnknown() && !plan.Source.Equal(state.Source) {
		body.Source = nullable.NewNullableWithValue(api.UpdateOrgBlueprintBodySource(plan.Source.ValueString()))
		// Switching to database keeps the git YAML unless contents are sent.
		if plan.Source.ValueString() == "database" && !plan.Contents.IsNull() {
			body.Contents = nullable.NewNullableWithValue(plan.Contents.ValueString())
		}
	}
	if !plan.BuildOnDefaultBranchCommit.IsUnknown() && !plan.BuildOnDefaultBranchCommit.Equal(state.BuildOnDefaultBranchCommit) {
		body.BuildOnDefaultBranchCommit = optionalBoolFrom(plan.BuildOnDefaultBranchCommit)
	}
	if !plan.ReportCheckRuns.IsUnknown() && !plan.ReportCheckRuns.Equal(state.ReportCheckRuns) {
		body.ReportCheckRuns = optionalBoolFrom(plan.ReportCheckRuns)
	}

	if !plan.Position.IsNull() && !plan.Position.IsUnknown() && !plan.Position.Equal(state.Position) {
		body.Position = optionalIntFrom(plan.Position)
	}

	var result api.Blueprint
	blueprintPath := orgBlueprintPath(plan.OrgID.ValueString(), plan.BlueprintID.ValueString())
	var err error
	if body.Contents.IsSpecified() || body.Source.IsSpecified() || body.BuildOnDefaultBranchCommit.IsSpecified() || body.ReportCheckRuns.IsSpecified() || body.Position.IsSpecified() {
		err = r.client.Patch(ctx, blueprintPath, body, &result)
	} else {
		err = r.client.Get(ctx, blueprintPath, &result)
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to update blueprint", err.Error())
		return
	}

	mapBlueprintToModel(&result, &plan)
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

func mapBlueprintToModel(result *api.Blueprint, model *blueprintModel) {
	model.BlueprintID = types.StringValue(result.BlueprintID)
	model.Type = types.StringValue(string(result.Type))
	// Devin normalizes the repo name; keep the configured spelling when it matches.
	repoName := stringFromNullable(result.RepoName)
	if repoName.IsNull() || !strings.EqualFold(repoName.ValueString(), model.RepoName.ValueString()) {
		model.RepoName = repoName
	}
	if result.Source.IsSpecified() && !result.Source.IsNull() {
		model.Source = types.StringValue(string(result.Source.MustGet()))
	} else {
		model.Source = types.StringNull()
	}
	model.BuildOnDefaultBranchCommit = boolFromNullable(result.BuildOnDefaultBranchCommit)
	model.ReportCheckRuns = boolFromNullable(result.ReportCheckRuns)
}

// readBlueprintContents fetches a blueprint's YAML through the presigned URL
// the contents endpoint returns. A blueprint with no saved contents reads as "".
func readBlueprintContents(ctx context.Context, client *Client, blueprintPath string, diags *diag.Diagnostics) (types.String, bool) {
	var download api.PresignedDownload
	err := client.Get(ctx, blueprintPath+"/contents", &download)
	if IsNotFound(err) {
		return types.StringValue(""), true
	}
	if err != nil {
		diags.AddError("Failed to read blueprint contents", err.Error())
		return types.StringNull(), false
	}
	contents, err := client.Download(ctx, download.URL)
	if err != nil {
		diags.AddError("Failed to download blueprint contents", err.Error())
		return types.StringNull(), false
	}
	return types.StringValue(contents), true
}
