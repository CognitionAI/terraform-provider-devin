package provider

import (
	"context"
	"net/url"
	"strconv"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &sessionsDataSource{}

type sessionsDataSource struct {
	client *Client
}

type sessionPullRequestModel struct {
	PrURL   types.String `tfsdk:"pr_url"`
	PrState types.String `tfsdk:"pr_state"`
}

type sessionModel struct {
	SessionID        types.String              `tfsdk:"session_id"`
	URL              types.String              `tfsdk:"url"`
	Title            types.String              `tfsdk:"title"`
	Status           types.String              `tfsdk:"status"`
	StatusDetail     types.String              `tfsdk:"status_detail"`
	Category         types.String              `tfsdk:"category"`
	Subcategory      types.String              `tfsdk:"subcategory"`
	Origin           types.String              `tfsdk:"origin"`
	DevinMode        types.String              `tfsdk:"devin_mode"`
	OrgID            types.String              `tfsdk:"org_id"`
	UserID           types.String              `tfsdk:"user_id"`
	ServiceUserID    types.String              `tfsdk:"service_user_id"`
	AutomationID     types.String              `tfsdk:"automation_id"`
	PlaybookID       types.String              `tfsdk:"playbook_id"`
	ParentSessionID  types.String              `tfsdk:"parent_session_id"`
	ChildSessionIDs  types.List                `tfsdk:"child_session_ids"`
	Tags             types.List                `tfsdk:"tags"`
	IsArchived       types.Bool                `tfsdk:"is_archived"`
	AcusConsumed     types.Float64             `tfsdk:"acus_consumed"`
	OnDemandUsage    types.Float64             `tfsdk:"on_demand_usage"`
	CreatedAt        types.Int64               `tfsdk:"created_at"`
	UpdatedAt        types.Int64               `tfsdk:"updated_at"`
	PullRequests     []sessionPullRequestModel `tfsdk:"pull_requests"`
	StructuredOutput jsontypes.Normalized      `tfsdk:"structured_output"`
}

type sessionsDataSourceModel struct {
	OrgID           types.String   `tfsdk:"org_id"`
	SessionIDs      types.Set      `tfsdk:"session_ids"`
	Tags            types.Set      `tfsdk:"tags"`
	UserIDs         types.Set      `tfsdk:"user_ids"`
	ServiceUserIDs  types.Set      `tfsdk:"service_user_ids"`
	AutomationIDs   types.Set      `tfsdk:"automation_ids"`
	RepoNames       types.Set      `tfsdk:"repo_names"`
	Origins         types.Set      `tfsdk:"origins"`
	Statuses        types.Set      `tfsdk:"statuses"`
	Category        types.String   `tfsdk:"category"`
	IsArchived      types.Bool     `tfsdk:"is_archived"`
	ParentSessionID types.String   `tfsdk:"parent_session_id"`
	PlaybookID      types.String   `tfsdk:"playbook_id"`
	ScheduleID      types.String   `tfsdk:"schedule_id"`
	DevinID         types.String   `tfsdk:"devin_id"`
	CreatedAfter    types.Int64    `tfsdk:"created_after"`
	CreatedBefore   types.Int64    `tfsdk:"created_before"`
	UpdatedAfter    types.Int64    `tfsdk:"updated_after"`
	UpdatedBefore   types.Int64    `tfsdk:"updated_before"`
	Limit           types.Int64    `tfsdk:"limit"`
	Sessions        []sessionModel `tfsdk:"sessions"`
}

func NewSessionsDataSource() datasource.DataSource {
	return &sessionsDataSource{}
}

func (d *sessionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sessions"
}

func sessionOriginValues() []string {
	return []string{
		string(api.SessionsQueryParamsOriginsAPI),
		string(api.SessionsQueryParamsOriginsAutomation),
		string(api.SessionsQueryParamsOriginsCli),
		string(api.SessionsQueryParamsOriginsCodeScan),
		string(api.SessionsQueryParamsOriginsDesktop),
		string(api.SessionsQueryParamsOriginsJira),
		string(api.SessionsQueryParamsOriginsLinear),
		string(api.SessionsQueryParamsOriginsOther),
		string(api.SessionsQueryParamsOriginsSlack),
		string(api.SessionsQueryParamsOriginsTeams),
		string(api.SessionsQueryParamsOriginsWebapp),
	}
}

func sessionStatusValues() []string {
	return []string{
		string(api.SessionResponseStatusNew),
		string(api.SessionResponseStatusClaimed),
		string(api.SessionResponseStatusRunning),
		string(api.SessionResponseStatusResuming),
		string(api.SessionResponseStatusSuspended),
		string(api.SessionResponseStatusExit),
		string(api.SessionResponseStatusError),
	}
}

func sessionCategoryValues() []string {
	return []string{
		string(api.SessionsQueryParamsCategoryBugFixing),
		string(api.SessionsQueryParamsCategoryCiCdAndDevops),
		string(api.SessionsQueryParamsCategoryCodeQualityAndSecurity),
		string(api.SessionsQueryParamsCategoryCodeReview),
		string(api.SessionsQueryParamsCategoryCodeReviewAndAnalysis),
		string(api.SessionsQueryParamsCategoryDataAndAutomation),
		string(api.SessionsQueryParamsCategoryDocumentationAndContent),
		string(api.SessionsQueryParamsCategoryFeatureDevelopment),
		string(api.SessionsQueryParamsCategoryMigrationsAndUpgrades),
		string(api.SessionsQueryParamsCategoryOther),
		string(api.SessionsQueryParamsCategoryProductionInvestigation),
		string(api.SessionsQueryParamsCategoryRefactoringAndOptimization),
		string(api.SessionsQueryParamsCategoryResearchAndExploration),
		string(api.SessionsQueryParamsCategorySecurity),
		string(api.SessionsQueryParamsCategoryUnitTestGeneration),
	}
}

func (d *sessionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Description: description, Computed: true}
	}
	optionalString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Description: description, Optional: true}
	}
	optionalStringSet := func(description string) schema.SetAttribute {
		return schema.SetAttribute{Description: description, Optional: true, ElementType: types.StringType}
	}
	optionalUnix := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Description: description + " (Unix seconds).", Optional: true}
	}
	resp.Schema = schema.Schema{
		Description: "Lists the sessions of an organization, with optional filters. Sessions are read-only " +
			"through this data source; use the API to create or message them.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID.",
				Required:    true,
			},
			"session_ids":       optionalStringSet("Only return these session IDs."),
			"tags":              optionalStringSet("Only return sessions carrying these tags."),
			"user_ids":          optionalStringSet("Only return sessions created by these users."),
			"service_user_ids":  optionalStringSet("Only return sessions created by these service users."),
			"automation_ids":    optionalStringSet("Only return sessions created by these automations."),
			"repo_names":        optionalStringSet("Only return sessions attached to these repositories (e.g. owner/repo)."),
			"parent_session_id": optionalString("Only return child sessions of this parent session."),
			"playbook_id":       optionalString("Only return sessions started from this playbook."),
			"schedule_id":       optionalString("Only return sessions started from this schedule."),
			"devin_id":          optionalString("Only return the session with this Devin ID."),
			"category": schema.StringAttribute{
				Description: "Only return sessions assigned this use-case category.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf(sessionCategoryValues()...),
				},
			},
			"origins": schema.SetAttribute{
				Description: "Only return sessions created through these origins.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.OneOf(sessionOriginValues()...)),
				},
			},
			"statuses": schema.SetAttribute{
				Description: "Only return sessions in these statuses (new, claimed, running, resuming, " +
					"suspended, exit, error). Applied client-side: the API does not filter by status, so " +
					"matching sessions are selected from the fetched pages.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.OneOf(sessionStatusValues()...)),
				},
			},
			"is_archived": schema.BoolAttribute{
				Description: "Only return archived (true) or unarchived (false) sessions.",
				Optional:    true,
			},
			"created_after":  optionalUnix("Only return sessions created after this time"),
			"created_before": optionalUnix("Only return sessions created before this time"),
			"updated_after":  optionalUnix("Only return sessions updated after this time"),
			"updated_before": optionalUnix("Only return sessions updated before this time"),
			"limit": schema.Int64Attribute{
				Description: "Maximum number of sessions to return. Sessions are ordered most recently " +
					"updated first, so a limit keeps the newest ones. Without a limit all matching " +
					"sessions are fetched, which can be slow and produce large state on big organizations.",
				Optional: true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"sessions": schema.ListNestedAttribute{
				Description: "Sessions matching the filters, most recently updated first.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"session_id":        computedString("Session ID."),
						"url":               computedString("Session URL in the Devin web app."),
						"title":             computedString("Session title."),
						"status":            computedString("Session status: new, claimed, running, resuming, suspended, exit, or error."),
						"status_detail":     computedString("Additional detail about the current status (e.g. working, waiting_for_user, finished, inactivity)."),
						"category":          computedString("Assigned use-case category, when categorisation has run."),
						"subcategory":       computedString("Assigned subcategory display name."),
						"origin":            computedString("Origin the session was created through (e.g. webapp, api, automation)."),
						"devin_mode":        computedString("Devin agent mode the session runs (e.g. normal, fast, swe-2-high)."),
						"org_id":            computedString("Owning organization. Null for detached sessions owned by the creator's account."),
						"user_id":           computedString("User that created the session."),
						"service_user_id":   computedString("Service user that created the session."),
						"automation_id":     computedString("Automation that created the session, if any."),
						"playbook_id":       computedString("Playbook the session was started from, if any."),
						"parent_session_id": computedString("Parent session that spawned this session, if any."),
						"child_session_ids": schema.ListAttribute{
							Description: "Sessions spawned by this session.",
							Computed:    true,
							ElementType: types.StringType,
						},
						"tags": schema.ListAttribute{
							Description: "Tags attached to the session.",
							Computed:    true,
							ElementType: types.StringType,
						},
						"is_archived": schema.BoolAttribute{
							Description: "Whether the session is archived.",
							Computed:    true,
						},
						"acus_consumed": schema.Float64Attribute{
							Description: "ACUs consumed by the session.",
							Computed:    true,
						},
						"on_demand_usage": schema.Float64Attribute{
							Description: "On-demand (overage) spend in USD, excluding child sessions. Only populated for Teams plans.",
							Computed:    true,
						},
						"created_at": schema.Int64Attribute{
							Description: "Creation time (Unix seconds).",
							Computed:    true,
						},
						"updated_at": schema.Int64Attribute{
							Description: "Last update time (Unix seconds).",
							Computed:    true,
						},
						"pull_requests": schema.ListNestedAttribute{
							Description: "Pull requests produced by the session.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"pr_url":   computedString("Pull request URL."),
									"pr_state": computedString("Pull request state (e.g. open, merged, closed)."),
								},
							},
						},
						"structured_output": schema.StringAttribute{
							Description: "Validated structured output from the session, as a JSON string.",
							Computed:    true,
							CustomType:  jsontypes.NormalizedType{},
						},
					},
				},
			},
		},
	}
}

func (d *sessionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *sessionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state sessionsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Sessions = []sessionModel{}

	sessionIDs := setToStrings(ctx, state.SessionIDs, &resp.Diagnostics)
	tags := setToStrings(ctx, state.Tags, &resp.Diagnostics)
	userIDs := setToStrings(ctx, state.UserIDs, &resp.Diagnostics)
	serviceUserIDs := setToStrings(ctx, state.ServiceUserIDs, &resp.Diagnostics)
	automationIDs := setToStrings(ctx, state.AutomationIDs, &resp.Diagnostics)
	repoNames := setToStrings(ctx, state.RepoNames, &resp.Diagnostics)
	origins := setToStrings(ctx, state.Origins, &resp.Diagnostics)
	statusFilter := setToStrings(ctx, state.Statuses, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	wantStatus := map[string]bool{}
	for _, s := range statusFilter {
		wantStatus[s] = true
	}

	query := url.Values{}
	query.Set("first", "100")
	for _, v := range sessionIDs {
		query.Add("session_ids", v)
	}
	for _, v := range tags {
		query.Add("tags", v)
	}
	for _, v := range userIDs {
		query.Add("user_ids", v)
	}
	for _, v := range serviceUserIDs {
		query.Add("service_user_ids", v)
	}
	for _, v := range automationIDs {
		query.Add("automation_ids", v)
	}
	for _, v := range repoNames {
		query.Add("repo_names", v)
	}
	for _, v := range origins {
		query.Add("origins", v)
	}
	setQueryParam := func(name string, value types.String) {
		if !value.IsNull() {
			query.Set(name, value.ValueString())
		}
	}
	setQueryParam("category", state.Category)
	setQueryParam("parent_session_id", state.ParentSessionID)
	setQueryParam("playbook_id", state.PlaybookID)
	setQueryParam("schedule_id", state.ScheduleID)
	setQueryParam("devin_id", state.DevinID)
	if !state.IsArchived.IsNull() {
		query.Set("is_archived", strconv.FormatBool(state.IsArchived.ValueBool()))
	}
	setUnixParam := func(name string, value types.Int64) {
		if !value.IsNull() {
			query.Set(name, strconv.FormatInt(value.ValueInt64(), 10))
		}
	}
	setUnixParam("created_after", state.CreatedAfter)
	setUnixParam("created_before", state.CreatedBefore)
	setUnixParam("updated_after", state.UpdatedAfter)
	setUnixParam("updated_before", state.UpdatedBefore)

	limit := int64(-1) // no limit
	if !state.Limit.IsNull() {
		limit = state.Limit.ValueInt64()
	}

	base := orgSessionsPath(state.OrgID.ValueString()) + "?" + query.Encode()
	for {
		var page api.PaginatedResponseSessionResponse
		if err := d.client.Get(ctx, base, &page); err != nil {
			resp.Diagnostics.AddError("Failed to list sessions", err.Error())
			return
		}

		for _, item := range page.Items {
			if limit >= 0 && int64(len(state.Sessions)) >= limit {
				break
			}
			if len(wantStatus) > 0 && !wantStatus[string(item.Status)] {
				continue
			}
			childIDs := types.ListNull(types.StringType)
			if item.ChildSessionIds.IsSpecified() && !item.ChildSessionIds.IsNull() {
				var dgs diag.Diagnostics
				childIDs, dgs = types.ListValueFrom(ctx, types.StringType, item.ChildSessionIds.MustGet())
				resp.Diagnostics.Append(dgs...)
			}
			tagList, dgs := types.ListValueFrom(ctx, types.StringType, item.Tags)
			resp.Diagnostics.Append(dgs...)
			prs := make([]sessionPullRequestModel, 0, len(item.PullRequests))
			for _, pr := range item.PullRequests {
				prs = append(prs, sessionPullRequestModel{
					PrURL:   types.StringValue(pr.PrURL),
					PrState: stringFromNullable(pr.PrState),
				})
			}
			state.Sessions = append(state.Sessions, sessionModel{
				SessionID:        types.StringValue(item.SessionID),
				URL:              types.StringValue(item.URL),
				Title:            stringFromNullable(item.Title),
				Status:           types.StringValue(string(item.Status)),
				StatusDetail:     enumStringFromNullable(item.StatusDetail),
				Category:         enumStringFromNullable(item.Category),
				Subcategory:      stringFromNullable(item.Subcategory),
				Origin:           enumStringFromNullable(item.Origin),
				DevinMode:        enumStringFromNullable(item.DevinMode),
				OrgID:            stringFromNullable(item.OrgID),
				UserID:           stringFromNullable(item.UserID),
				ServiceUserID:    stringFromNullable(item.ServiceUserID),
				AutomationID:     stringFromNullable(item.AutomationID),
				PlaybookID:       stringFromNullable(item.PlaybookID),
				ParentSessionID:  stringFromNullable(item.ParentSessionID),
				ChildSessionIDs:  childIDs,
				Tags:             tagList,
				IsArchived:       types.BoolValue(item.IsArchived != nil && *item.IsArchived),
				AcusConsumed:     types.Float64Value(float64(item.AcusConsumed)),
				OnDemandUsage:    float64FromNullable(item.OnDemandUsage),
				CreatedAt:        types.Int64Value(int64(item.CreatedAt)),
				UpdatedAt:        types.Int64Value(int64(item.UpdatedAt)),
				PullRequests:     prs,
				StructuredOutput: jsonObjectFromNullable(item.StructuredOutput, "structured_output", &resp.Diagnostics),
			})
		}
		if resp.Diagnostics.HasError() {
			return
		}

		if limit >= 0 && int64(len(state.Sessions)) >= limit {
			break
		}
		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			break
		}
		query.Set("after", page.EndCursor.MustGet())
		base = orgSessionsPath(state.OrgID.ValueString()) + "?" + query.Encode()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
