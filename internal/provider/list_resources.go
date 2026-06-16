package provider

import (
	"context"
	"net/url"
	"strconv"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// List resource implementations for `terraform query` (Terraform 1.14+).
// Each one targets the managed resource of the same type name and emits that
// resource's identity (see identity.go) plus, when requested, the full
// resource attributes.

// listClient provides the shared Configure implementation for list resources.
type listClient struct {
	client *Client
}

func (l *listClient) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	l.client = req.ProviderData.(*Client)
}

// orgScopedListSchema is the list config schema for resources that live
// inside a single organization.
func orgScopedListSchema(description string) listschema.Schema {
	return listschema.Schema{
		Description: description,
		Attributes: map[string]listschema.Attribute{
			"org_id": listschema.StringAttribute{
				Description: "Organization ID to list resources from.",
				Required:    true,
			},
		},
	}
}

type orgScopedListConfig struct {
	OrgID types.String `tfsdk:"org_id"`
}

// pushListResult assembles a ListResult from an identity and optional
// resource model and pushes it onto the stream. It returns false when the
// consumer stopped the iteration.
func pushListResult(ctx context.Context, req list.ListRequest, push func(list.ListResult) bool, displayName string, identity any, resourceModel any) bool {
	result := req.NewListResult(ctx)
	result.DisplayName = displayName
	result.Diagnostics.Append(result.Identity.Set(ctx, identity)...)
	if req.IncludeResource {
		result.Diagnostics.Append(result.Resource.Set(ctx, resourceModel)...)
	} else {
		result.Resource = nil
	}
	return push(result)
}

func pushListError(ctx context.Context, req list.ListRequest, push func(list.ListResult) bool, summary string, err error) {
	result := req.NewListResult(ctx)
	result.Resource = nil
	result.Identity = nil
	result.Diagnostics.AddError(summary, err.Error())
	push(result)
}

// hasNextPage reports whether a paginated response has more pages and, if so,
// returns the next cursor.
func nextCursor(hasNext *bool, endCursor interface {
	IsSpecified() bool
	IsNull() bool
	MustGet() string
}) (string, bool) {
	if hasNext == nil || !*hasNext || !endCursor.IsSpecified() || endCursor.IsNull() {
		return "", false
	}
	return endCursor.MustGet(), true
}

func pageQuery(cursor string) string {
	query := url.Values{}
	query.Set("first", "100")
	if cursor != "" {
		query.Set("after", cursor)
	}
	return "?" + query.Encode()
}

// --- devin_organization ---

type organizationListResource struct {
	listClient
}

func NewOrganizationListResource() list.ListResource {
	return &organizationListResource{}
}

func (l *organizationListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (l *organizationListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the organizations in the enterprise.",
	}
}

func (l *organizationListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponseOrganizationResponse
			if err := l.client.Get(ctx, enterpriseOrganizationsPath+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list organizations", err)
				return
			}
			for i := range page.Items {
				var model organizationModel
				mapOrgResponseToModel(&page.Items[i], &model)
				if !pushListResult(ctx, req, push, page.Items[i].Name,
					organizationIdentityModel{OrgID: model.OrgID}, model) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}

// --- devin_playbook ---

type playbookListResource struct {
	listClient
}

func NewPlaybookListResource() list.ListResource {
	return &playbookListResource{}
}

func (l *playbookListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_playbook"
}

func (l *playbookListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = orgScopedListSchema("Lists the playbooks in an organization.")
}

func (l *playbookListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var config orgScopedListConfig
	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponsePlaybookResponse
			if err := l.client.Get(ctx, orgPlaybooksPath(config.OrgID.ValueString())+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list playbooks", err)
				return
			}
			for i := range page.Items {
				model := playbookModel{OrgID: config.OrgID}
				mapPlaybookResponseToModel(&page.Items[i], &model)
				if !pushListResult(ctx, req, push, page.Items[i].Title,
					playbookIdentityModel{OrgID: model.OrgID, PlaybookID: model.PlaybookID}, model) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}

// --- devin_enterprise_playbook ---

type enterprisePlaybookListResource struct {
	listClient
}

func NewEnterprisePlaybookListResource() list.ListResource {
	return &enterprisePlaybookListResource{}
}

func (l *enterprisePlaybookListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_playbook"
}

func (l *enterprisePlaybookListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the enterprise-level playbooks.",
	}
}

func (l *enterprisePlaybookListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponsePlaybookResponse
			if err := l.client.Get(ctx, enterprisePlaybooksPath+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list enterprise playbooks", err)
				return
			}
			for i := range page.Items {
				// The enterprise endpoint also returns org-scoped playbooks.
				if page.Items[i].AccessType != "enterprise" {
					continue
				}
				var model enterprisePlaybookModel
				mapEnterprisePlaybookResponseToModel(&page.Items[i], &model)
				if !pushListResult(ctx, req, push, page.Items[i].Title,
					enterprisePlaybookIdentityModel{PlaybookID: model.PlaybookID}, model) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}

// --- devin_knowledge_note ---

type knowledgeNoteListResource struct {
	listClient
}

func NewKnowledgeNoteListResource() list.ListResource {
	return &knowledgeNoteListResource{}
}

func (l *knowledgeNoteListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_knowledge_note"
}

func (l *knowledgeNoteListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = orgScopedListSchema("Lists the knowledge notes in an organization.")
}

func (l *knowledgeNoteListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var config orgScopedListConfig
	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponseKnowledgeNoteResponse
			if err := l.client.Get(ctx, orgKnowledgeNotesPath(config.OrgID.ValueString())+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list knowledge notes", err)
				return
			}
			for i := range page.Items {
				model := knowledgeNoteModel{OrgID: config.OrgID}
				mapKnowledgeNoteResponseToModel(&page.Items[i], &model)
				if !pushListResult(ctx, req, push, page.Items[i].Name,
					knowledgeNoteIdentityModel{OrgID: model.OrgID, NoteID: model.NoteID}, model) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}

// --- devin_enterprise_knowledge_note ---

type enterpriseKnowledgeNoteListResource struct {
	listClient
}

func NewEnterpriseKnowledgeNoteListResource() list.ListResource {
	return &enterpriseKnowledgeNoteListResource{}
}

func (l *enterpriseKnowledgeNoteListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_knowledge_note"
}

func (l *enterpriseKnowledgeNoteListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the enterprise-level knowledge notes.",
	}
}

func (l *enterpriseKnowledgeNoteListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponseKnowledgeNoteResponse
			if err := l.client.Get(ctx, enterpriseKnowledgeNotesPath+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list enterprise knowledge notes", err)
				return
			}
			for i := range page.Items {
				// The enterprise endpoint also returns org-scoped notes.
				if page.Items[i].AccessType != "enterprise" {
					continue
				}
				var model enterpriseKnowledgeNoteModel
				mapEnterpriseKnowledgeNoteResponseToModel(&page.Items[i], &model)
				if !pushListResult(ctx, req, push, page.Items[i].Name,
					enterpriseKnowledgeNoteIdentityModel{NoteID: model.NoteID}, model) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}

// --- devin_secret ---

type secretListResource struct {
	listClient
}

func NewSecretListResource() list.ListResource {
	return &secretListResource{}
}

func (l *secretListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (l *secretListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = orgScopedListSchema("Lists the secrets in an organization for discovery and inventory. Secret values are never returned by the API, so listed secrets cannot be imported.")
}

func (l *secretListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var config orgScopedListConfig
	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponseSecretResponse
			if err := l.client.Get(ctx, orgSecretsPath(config.OrgID.ValueString())+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list secrets", err)
				return
			}
			for i := range page.Items {
				model := secretModel{OrgID: config.OrgID}
				mapSecretResponseToModel(&page.Items[i], &model)
				displayName := model.Key.ValueString()
				if displayName == "" {
					displayName = model.SecretID.ValueString()
				}
				if !pushListResult(ctx, req, push, displayName,
					secretIdentityModel{OrgID: model.OrgID, SecretID: model.SecretID}, model) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}

// --- devin_schedule ---

type scheduleListResource struct {
	listClient
}

func NewScheduleListResource() list.ListResource {
	return &scheduleListResource{}
}

func (l *scheduleListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func (l *scheduleListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = orgScopedListSchema("Lists the session schedules in an organization.")
}

func (l *scheduleListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var config orgScopedListConfig
	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		offset := 0
		// The schedules endpoint uses limit/offset pagination, unlike the
		// cursor-based list endpoints.
		for {
			query := url.Values{}
			query.Set("limit", "200")
			if offset > 0 {
				query.Set("offset", strconv.Itoa(offset))
			}
			var page api.PaginatedResponseScheduleResponse
			if err := l.client.Get(ctx, orgSchedulesPath(config.OrgID.ValueString())+"?"+query.Encode(), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list schedules", err)
				return
			}
			for i := range page.Items {
				var model scheduleModel
				result := req.NewListResult(ctx)
				result.DisplayName = page.Items[i].Name
				result.Diagnostics.Append(mapScheduleResponseToModel(ctx, &page.Items[i], &model)...)
				result.Diagnostics.Append(result.Identity.Set(ctx, scheduleIdentityModel{OrgID: model.OrgID, ScheduleID: model.ScheduleID})...)
				if req.IncludeResource {
					result.Diagnostics.Append(result.Resource.Set(ctx, model)...)
				} else {
					result.Resource = nil
				}
				if !push(result) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			if page.HasNextPage == nil || !*page.HasNextPage || len(page.Items) == 0 {
				return
			}
			offset += len(page.Items)
		}
	}
}

// --- devin_idp_group ---

type idpGroupListResource struct {
	listClient
}

func NewIdpGroupListResource() list.ListResource {
	return &idpGroupListResource{}
}

func (l *idpGroupListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_idp_group"
}

func (l *idpGroupListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the IdP groups registered with the enterprise.",
	}
}

func (l *idpGroupListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponseIdpGroupResponse
			if err := l.client.Get(ctx, enterpriseIdpGroupsPath+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list IdP groups", err)
				return
			}
			for _, item := range page.Items {
				name := types.StringValue(item.IdpGroupName)
				if !pushListResult(ctx, req, push, item.IdpGroupName,
					idpGroupIdentityModel{Name: name}, idpGroupModel{Name: name}) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}

// --- devin_git_permission ---

type gitPermissionListResource struct {
	listClient
}

func NewGitPermissionListResource() list.ListResource {
	return &gitPermissionListResource{}
}

func (l *gitPermissionListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_git_permission"
}

func (l *gitPermissionListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = orgScopedListSchema("Lists the git permissions of an organization.")
}

func (l *gitPermissionListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var config orgScopedListConfig
	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		count := int64(0)
		cursor := ""
		for {
			var page api.PaginatedResponseGitPermissionResponse
			if err := l.client.Get(ctx, orgGitPermissionsPath(config.OrgID.ValueString())+pageQuery(cursor), &page); err != nil {
				pushListError(ctx, req, push, "Failed to list git permissions", err)
				return
			}
			for i := range page.Items {
				model := gitPermissionModel{OrgID: config.OrgID}
				mapGitPermResponseToModel(&page.Items[i], &model)
				displayName := model.RepoPath.ValueString()
				if displayName == "" {
					displayName = model.GroupPrefix.ValueString()
				}
				if displayName == "" {
					displayName = model.PrefixPath.ValueString()
				}
				if displayName == "" {
					displayName = model.GitPermissionID.ValueString()
				}
				if !pushListResult(ctx, req, push, displayName,
					gitPermissionIdentityModel{OrgID: model.OrgID, GitPermissionID: model.GitPermissionID}, model) {
					return
				}
				if count++; req.Limit > 0 && count >= req.Limit {
					return
				}
			}
			next, ok := nextCursor(page.HasNextPage, page.EndCursor)
			if !ok {
				return
			}
			cursor = next
		}
	}
}
