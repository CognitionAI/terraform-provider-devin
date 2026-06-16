package provider

import (
	"context"
	"net/url"
	"regexp"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &gitPermissionResource{}
var _ resource.ResourceWithImportState = &gitPermissionResource{}
var _ resource.ResourceWithIdentity = &gitPermissionResource{}
var _ resource.ResourceWithConfigValidators = &gitPermissionResource{}

type gitPermissionResource struct {
	client *Client
}

type gitPermissionModel struct {
	GitPermissionID types.String `tfsdk:"git_permission_id"`
	OrgID           types.String `tfsdk:"org_id"`
	GitConnectionID types.String `tfsdk:"git_connection_id"`
	RepoPath        types.String `tfsdk:"repo_path"`
	GroupPrefix     types.String `tfsdk:"group_prefix"`
	PrefixPath      types.String `tfsdk:"prefix_path"`
	ReadOnly        types.Bool   `tfsdk:"read_only"`
}

func NewGitPermissionResource() resource.Resource {
	return &gitPermissionResource{}
}

func (r *gitPermissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_git_permission"
}

func (r *gitPermissionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a git permission for a Devin organization. Exactly one of repo_path, group_prefix, or prefix_path must be set.",
		Attributes: map[string]schema.Attribute{
			"git_permission_id": schema.StringAttribute{
				Description: "Git permission ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID to assign the permission to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"git_connection_id": schema.StringAttribute{
				Description: "Git connection ID for the provider (GitHub, GitLab, etc.).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"repo_path": schema.StringAttribute{
				Description: "Exact repository path (e.g., 'myorg/myrepo'). Mutually exclusive with group_prefix and prefix_path.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"group_prefix": schema.StringAttribute{
				Description: "Repository group/org prefix (e.g., 'myorg'). Grants access to all repos under this prefix. Must not end with a slash. Mutually exclusive with repo_path and prefix_path.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				// The API stores group prefixes with a trailing slash appended and
				// returns them without one, so a trailing slash in the config would
				// never match the value read back.
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`[^/]$`),
						"must not end with a slash",
					),
				},
			},
			"prefix_path": schema.StringAttribute{
				Description: "Path prefix for matching repositories. Mutually exclusive with repo_path and group_prefix.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"read_only": schema.BoolAttribute{
				Description: "Whether the permission grants read-only access.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

func (r *gitPermissionResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("repo_path"),
			path.MatchRoot("group_prefix"),
			path.MatchRoot("prefix_path"),
		),
	}
}

func (r *gitPermissionResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = gitPermissionIdentitySchema()
}

func (r *gitPermissionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *gitPermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan gitPermissionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.GitPermissionBulkCreateRequest{
		Permissions: []api.GitPermissionCreateRequest{{
			GitConnectionID: plan.GitConnectionID.ValueString(),
			ReadOnly:        boolPtrFrom(plan.ReadOnly),
			RepoPath:        optionalStringFrom(plan.RepoPath),
			GroupPrefix:     optionalStringFrom(plan.GroupPrefix),
			PrefixPath:      optionalStringFrom(plan.PrefixPath),
		}},
	}

	var result []api.GitPermissionResponse
	err := r.client.Post(ctx, orgGitPermissionsPath(plan.OrgID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create git permission", err.Error())
		return
	}

	if len(result) == 0 {
		resp.Diagnostics.AddError("Failed to create git permission", "empty response from API")
		return
	}

	mapGitPermResponseToModel(&result[0], &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, gitPermissionIdentityModel{OrgID: plan.OrgID, GitPermissionID: plan.GitPermissionID}, &resp.Diagnostics)
}

func (r *gitPermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state gitPermissionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API only exposes a paginated list endpoint; walk the pages until the
	// permission is found or the list is exhausted.
	var found *api.GitPermissionResponse
	cursor := ""
	for {
		apiPath := orgGitPermissionsPath(state.OrgID.ValueString()) + "?first=100"
		if cursor != "" {
			apiPath += "&after=" + url.QueryEscape(cursor)
		}

		var result api.PaginatedResponseGitPermissionResponse
		err := r.client.Get(ctx, apiPath, &result)
		if IsNotFound(err) {
			setIdentity(ctx, resp.Identity, gitPermissionIdentityModel{OrgID: state.OrgID, GitPermissionID: state.GitPermissionID}, &resp.Diagnostics)
			resp.State.RemoveResource(ctx)
			return
		}
		if err != nil {
			resp.Diagnostics.AddError("Failed to read git permissions", err.Error())
			return
		}

		for i := range result.Items {
			if result.Items[i].GitPermissionID == state.GitPermissionID.ValueString() {
				found = &result.Items[i]
				break
			}
		}
		hasNext := result.HasNextPage != nil && *result.HasNextPage
		if found != nil || !hasNext || !result.EndCursor.IsSpecified() || result.EndCursor.IsNull() {
			break
		}
		cursor = result.EndCursor.MustGet()
	}
	if found == nil {
		setIdentity(ctx, resp.Identity, gitPermissionIdentityModel{OrgID: state.OrgID, GitPermissionID: state.GitPermissionID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}

	mapGitPermResponseToModel(found, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, gitPermissionIdentityModel{OrgID: state.OrgID, GitPermissionID: state.GitPermissionID}, &resp.Diagnostics)
}

func (r *gitPermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan gitPermissionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.GitPermissionUpdateRequest{
		ReadOnly: nullable.NewNullableWithValue(plan.ReadOnly.ValueBool()),
	}

	var result api.GitPermissionResponse
	err := r.client.Patch(ctx, orgGitPermissionPath(plan.OrgID.ValueString(), plan.GitPermissionID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update git permission", err.Error())
		return
	}

	mapGitPermResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, gitPermissionIdentityModel{OrgID: plan.OrgID, GitPermissionID: plan.GitPermissionID}, &resp.Diagnostics)
}

func (r *gitPermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state gitPermissionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, orgGitPermissionPath(state.OrgID.ValueString(), state.GitPermissionID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete git permission", err.Error())
	}
}

func (r *gitPermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "git_permission_id")
}

func mapGitPermResponseToModel(resp *api.GitPermissionResponse, model *gitPermissionModel) {
	model.GitPermissionID = types.StringValue(resp.GitPermissionID)
	model.GitConnectionID = types.StringValue(resp.GitConnectionID)
	model.ReadOnly = types.BoolValue(resp.ReadOnly != nil && *resp.ReadOnly)
	model.RepoPath = stringFromNullable(resp.RepoPath)
	model.GroupPrefix = stringFromNullable(resp.GroupPrefix)
	model.PrefixPath = stringFromNullable(resp.PrefixPath)
}
