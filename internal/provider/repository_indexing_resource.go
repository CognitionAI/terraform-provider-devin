package provider

import (
	"context"
	"net/url"
	"strings"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &repositoryIndexingResource{}
var _ resource.ResourceWithImportState = &repositoryIndexingResource{}
var _ resource.ResourceWithIdentity = &repositoryIndexingResource{}

type repositoryIndexingResource struct {
	client *Client
}

type repositoryIndexingModel struct {
	OrgID          types.String `tfsdk:"org_id"`
	RepositoryPath types.String `tfsdk:"repository_path"`
	BranchNames    types.Set    `tfsdk:"branch_names"`
	Branches       types.Set    `tfsdk:"branches"`
}

func NewRepositoryIndexingResource() resource.Resource {
	return &repositoryIndexingResource{}
}

func (r *repositoryIndexingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repository_indexing"
}

func (r *repositoryIndexingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Enables indexing (code search and wiki) for a connected repository in an organization. " +
			"The repository's default branch is always indexed. Destroying the resource turns indexing off " +
			"and clears its configured branches. Creating it, or adding branches, starts indexing jobs.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID to index the repository in.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"repository_path": schema.StringAttribute{
				Description: "Repository path, e.g. owner/repo. The organization must have access to the repository.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"branch_names": schema.SetAttribute{
				Description: "Branches to index in addition to the default branch.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"branches": schema.SetAttribute{
				Description: "All branches configured for indexing, as reported by Devin. " +
					"When branch_names is set, Devin also adds the default branch here.",
				ElementType: types.StringType,
				Computed:    true,
				PlanModifiers: []planmodifier.Set{
					branchesFollowBranchNamesModifier{},
				},
			},
		},
	}
}

func (r *repositoryIndexingResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = repositoryIndexingIdentitySchema()
}

func (r *repositoryIndexingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *repositoryIndexingResource) index(ctx context.Context, model *repositoryIndexingModel, branchNames []string, diags *diag.Diagnostics) bool {
	body := api.IndexRepositoryRequest{BranchNames: &branchNames}
	var result api.RepositoryIndexingResponse
	err := r.client.Put(ctx, orgRepositoryIndexingPath(model.OrgID.ValueString(), model.RepositoryPath.ValueString()), body, &result)
	if err != nil {
		diags.AddError("Failed to index repository", err.Error())
		return false
	}
	setRepositoryBranches(ctx, &result, model, diags)
	return !diags.HasError()
}

func (r *repositoryIndexingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan repositoryIndexingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	branchNames := setToStrings(ctx, plan.BranchNames, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !r.index(ctx, &plan, branchNames, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, repositoryIndexingIdentityModel{OrgID: plan.OrgID, RepositoryPath: plan.RepositoryPath}, &resp.Diagnostics)
}

func (r *repositoryIndexingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state repositoryIndexingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.findIndexedRepository(ctx, state.OrgID.ValueString(), state.RepositoryPath.ValueString())
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to read repository indexing", err.Error())
		return
	}
	identity := repositoryIndexingIdentityModel{OrgID: state.OrgID, RepositoryPath: state.RepositoryPath}
	if result == nil {
		setIdentity(ctx, resp.Identity, identity, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}

	setRepositoryBranches(ctx, result, &state, &resp.Diagnostics)
	// Keep only the configured branches Devin still indexes, so a branch
	// removed out of band shows up as drift. On import, adopt every branch.
	if state.BranchNames.IsNull() {
		if len(result.Branches) > 0 {
			state.BranchNames = state.Branches
		}
	} else {
		indexed := map[string]bool{}
		for _, b := range result.Branches {
			indexed[b] = true
		}
		var kept []string
		for _, b := range setToStrings(ctx, state.BranchNames, &resp.Diagnostics) {
			if indexed[b] {
				kept = append(kept, b)
			}
		}
		value, d := types.SetValueFrom(ctx, types.StringType, kept)
		resp.Diagnostics.Append(d...)
		state.BranchNames = value
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, identity, &resp.Diagnostics)
}

// findIndexedRepository pages through the org's indexed repositories (there
// is no GET endpoint that returns a repository's branches) and returns nil
// when the repository is not indexed.
func (r *repositoryIndexingResource) findIndexedRepository(ctx context.Context, orgID, repositoryPath string) (*api.RepositoryIndexingResponse, error) {
	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		query.Set("filter_name", encodeRepoPath(repositoryPath))
		if cursor != "" {
			query.Set("after", cursor)
		}
		var page api.PaginatedResponseRepositoryIndexingResponse
		if err := r.client.Get(ctx, orgIndexedRepositoriesPath(orgID)+"?"+query.Encode(), &page); err != nil {
			return nil, err
		}
		for i := range page.Items {
			if sameRepoPath(page.Items[i].RepositoryPath, repositoryPath) && page.Items[i].IndexingEnabled {
				return &page.Items[i], nil
			}
		}
		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			return nil, nil
		}
		cursor = page.EndCursor.MustGet()
	}
}

func (r *repositoryIndexingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state repositoryIndexingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	want := setToStrings(ctx, plan.BranchNames, &resp.Diagnostics)
	had := setToStrings(ctx, state.BranchNames, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	wantSet := map[string]bool{}
	for _, b := range want {
		wantSet[b] = true
	}
	hadSet := map[string]bool{}
	for _, b := range had {
		hadSet[b] = true
	}

	// Indexing a repository only adds branches, so removed branches are
	// deleted one by one.
	var result api.RepositoryIndexingResponse
	removed := false
	for _, b := range had {
		if wantSet[b] {
			continue
		}
		branchPath := orgRepositoryIndexingPath(plan.OrgID.ValueString(), plan.RepositoryPath.ValueString()) + "/branches/" + url.PathEscape(b)
		err := r.client.Delete(ctx, branchPath, &result)
		if IsNotFound(err) && strings.Contains(b, "/") && r.branchStillIndexed(ctx, &plan, b, &resp.Diagnostics) {
			// The API routes on the decoded path, so it can't match a branch
			// name containing "/" and answers 404 even though the branch exists.
			resp.Diagnostics.AddError("Failed to remove branch from indexing",
				"The Devin API can't remove indexed branches whose names contain \"/\" ("+b+"). "+
					"Remove it in the Devin UI, or destroy and recreate this resource.")
			return
		}
		if err != nil && !IsNotFound(err) {
			resp.Diagnostics.AddError("Failed to remove branch from indexing", err.Error())
			return
		}
		removed = removed || err == nil
	}

	// Every PUT starts indexing jobs, so it is only sent to add branches or to
	// turn indexing back on after the last branch was removed.
	added := false
	for _, b := range want {
		added = added || !hadSet[b]
	}
	if added || (removed && !result.IndexingEnabled) {
		if !r.index(ctx, &plan, want, &resp.Diagnostics) {
			return
		}
	} else if removed {
		setRepositoryBranches(ctx, &result, &plan, &resp.Diagnostics)
	} else {
		plan.Branches = state.Branches
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, repositoryIndexingIdentityModel{OrgID: plan.OrgID, RepositoryPath: plan.RepositoryPath}, &resp.Diagnostics)
}

// branchStillIndexed reports whether Devin still indexes the branch; a read
// failure is reported through diags and counts as indexed.
func (r *repositoryIndexingResource) branchStillIndexed(ctx context.Context, model *repositoryIndexingModel, branch string, diags *diag.Diagnostics) bool {
	result, err := r.findIndexedRepository(ctx, model.OrgID.ValueString(), model.RepositoryPath.ValueString())
	if err != nil {
		diags.AddError("Failed to read repository indexing", err.Error())
		return true
	}
	if result == nil {
		return false
	}
	for _, b := range result.Branches {
		if b == branch {
			return true
		}
	}
	return false
}

func (r *repositoryIndexingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state repositoryIndexingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Delete(ctx, orgRepositoryIndexingPath(state.OrgID.ValueString(), state.RepositoryPath.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to remove repository from indexing", err.Error())
	}
}

func (r *repositoryIndexingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The repository path contains slashes, so split off only the org ID.
	if req.ID != "" {
		orgID, repositoryPath, ok := strings.Cut(req.ID, "/")
		if !ok || orgID == "" || repositoryPath == "" {
			resp.Diagnostics.AddError("Unexpected import identifier", "Expected import identifier of the form \"org_id/owner/repo\", got: "+req.ID)
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("org_id"), types.StringValue(orgID))...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("repository_path"), types.StringValue(repositoryPath))...)
		setIdentity(ctx, resp.Identity, repositoryIndexingIdentityModel{OrgID: types.StringValue(orgID), RepositoryPath: types.StringValue(repositoryPath)}, &resp.Diagnostics)
		return
	}
	importComposite(ctx, req, resp, "org_id", "repository_path")
}

// branchesFollowBranchNamesModifier keeps the prior branches in the plan
// unless branch_names changes, in which case Devin reports a new set.
type branchesFollowBranchNamesModifier struct{}

func (branchesFollowBranchNamesModifier) Description(context.Context) string {
	return "Unchanged unless branch_names changes."
}

func (branchesFollowBranchNamesModifier) MarkdownDescription(context.Context) string {
	return "Unchanged unless branch_names changes."
}

func (branchesFollowBranchNamesModifier) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || req.StateValue.IsNull() || !req.PlanValue.IsUnknown() {
		return
	}
	var planNames, stateNames types.Set
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("branch_names"), &planNames)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("branch_names"), &stateNames)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if planNames.Equal(stateNames) {
		resp.PlanValue = req.StateValue
	}
}

func setRepositoryBranches(ctx context.Context, result *api.RepositoryIndexingResponse, model *repositoryIndexingModel, diags *diag.Diagnostics) {
	branches := result.Branches
	if branches == nil {
		branches = []string{}
	}
	value, d := types.SetValueFrom(ctx, types.StringType, branches)
	diags.Append(d...)
	model.Branches = value
}

// encodeRepoPath percent-encodes each segment of a repository path, the form
// the API stores repository names in ("acme/my repo" -> "acme/my%20repo").
func encodeRepoPath(repositoryPath string) string {
	segments := strings.Split(repositoryPath, "/")
	for i, segment := range segments {
		if decoded, err := url.PathUnescape(segment); err == nil {
			segment = decoded
		}
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// sameRepoPath compares repository paths regardless of percent-encoding and case.
func sameRepoPath(a, b string) bool {
	decode := func(p string) string {
		if decoded, err := url.PathUnescape(p); err == nil {
			return decoded
		}
		return p
	}
	return strings.EqualFold(decode(a), decode(b))
}
