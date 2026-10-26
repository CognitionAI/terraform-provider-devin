package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &oncallResponderResource{}
var _ resource.ResourceWithImportState = &oncallResponderResource{}
var _ resource.ResourceWithIdentity = &oncallResponderResource{}
var _ resource.ResourceWithValidateConfig = &oncallResponderResource{}

type oncallResponderResource struct {
	client *Client
}

// Like devin_automation, the triggers/tools/limits/session_settings/session groups are
// JSON-encoded and config-authoritative: Read does not overwrite them with the
// server's normalized echo. Scalars are refreshed on Read.
type oncallResponderModel struct {
	ResponderID           types.String         `tfsdk:"responder_id"`
	OrgID                 types.String         `tfsdk:"org_id"`
	Name                  types.String         `tfsdk:"name"`
	Enabled               types.Bool           `tfsdk:"enabled"`
	Runbook               types.String         `tfsdk:"runbook"`
	Metadata              types.Map            `tfsdk:"metadata"`
	Triggers              jsontypes.Normalized `tfsdk:"triggers"`
	Tools                 jsontypes.Normalized `tfsdk:"tools"`
	Limits                jsontypes.Normalized `tfsdk:"limits"`
	SessionSettings       jsontypes.Normalized `tfsdk:"session_settings"`
	Session               jsontypes.Normalized `tfsdk:"session"`
	TeamID                types.String         `tfsdk:"team_id"`
	PagerdutyConnectionID types.String         `tfsdk:"pagerduty_connection_id"`
	RunAs                 types.String         `tfsdk:"run_as"`
	RunAsServiceUser      types.String         `tfsdk:"run_as_service_user_id"`
	SlackReplyAccess      types.String         `tfsdk:"slack_reply_access"`
	TeamsReplyAccess      types.String         `tfsdk:"teams_reply_access"`
	TriageIssueType       types.String         `tfsdk:"triage_issue_type"`
	Source                types.String         `tfsdk:"source"`
	SourceChannelID       types.String         `tfsdk:"source_channel_id"`
	WebhookURL            types.String         `tfsdk:"webhook_url"`
	WebhookSecret         types.String         `tfsdk:"webhook_secret"`
}

func NewOncallResponderResource() resource.Resource {
	return &oncallResponderResource{}
}

func (r *oncallResponderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_oncall_responder"
}

func (r *oncallResponderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computedString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{
			Description:   description,
			Computed:      true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		Description: "Manages a Devin Oncall responder: an auto-triage automation that watches one alert " +
			"source (Slack channels or user-group mentions, Microsoft Teams channels, PagerDuty incidents, " +
			"or an incoming webhook) and starts a triage session for each new alert. The triggers/tools/" +
			"limits/session_settings/session groups are JSON-encoded (use jsonencode(...)); their configured values " +
			"are authoritative and out-of-band edits to them are not detected as drift. The devin_oncall_schemas " +
			"data source documents the supported trigger event types and condition fields.",
		Attributes: map[string]schema.Attribute{
			"responder_id": computedString("Responder ID (assigned by Devin)."),
			"org_id": schema.StringAttribute{
				Description:   "Organization ID that owns this responder.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Description: "Responder name.",
				Required:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the responder triages new alerts. Defaults to true.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"runbook": schema.StringAttribute{
				Description: "Triage instructions given to every session the responder starts. Defaults to " +
					"the organization's default runbook (see devin_oncall_schemas.default_runbook); omit it to use the default.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`\S`), "must not be blank; omit runbook to use the default runbook"),
				},
			},
			"metadata": schema.MapAttribute{
				ElementType: types.StringType,
				Description: "Org-visible key/value labels. At most 16 pairs; keys at most 32 chars; values at most 128.",
				Optional:    true,
			},
			"triggers": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded array of triggers (use jsonencode(...)). Each trigger has event_type " +
					"('slack:message', 'slack:usergroup_mentioned', 'teams:message', a 'pagerduty:incident_*' event, " +
					"or 'webhook:incoming') and optional conditions (two-level any/all envelope of {field, operator, " +
					"value}). All triggers of a responder use one provider; every Slack group needs a channel " +
					"restriction; every teams:message group needs team_id eq and channel_id eq/in; webhook:incoming " +
					"must be the only trigger and has no conditions; pagerduty:* triggers need pagerduty_connection_id.",
				Required: true,
			},
			"tools": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded tools object (use jsonencode(...)): mcp_servers, slack_channels " +
					"([{team_id, channel_id}]), teams_channels and linear_enabled granted to triage sessions.",
				Optional: true,
			},
			"limits": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded limits object (use jsonencode(...)): max_acu_limit (1-1000) and/or " +
					"invocations ({max_per_window >= 1, window_seconds >= 60}).",
				Optional: true,
			},
			"session_settings": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded settings for triage sessions (use jsonencode(...)): devin_mode " +
					"(normal, fast, lite, ultra, fusion; see devin_oncall_schemas.available_devin_modes) and net_policy.",
				Optional: true,
			},
			"session": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded session object (use jsonencode(...)): tags, the session tags (cost codes) " +
					"triage sessions bill to. At most 16 tags; organizations that enforce required session tags must " +
					"supply exactly one allowed tag.",
				Optional: true,
			},
			"team_id": schema.StringAttribute{
				Description: "Slack workspace (team) ID of the watched channels, for organizations with more than " +
					"one connected workspace. Slack responders only.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), webhookRecomputeModifier{}},
			},
			"pagerduty_connection_id": schema.StringAttribute{
				Description: "PagerDuty connection the pagerduty:* triggers listen on " +
					"(see devin_oncall_schemas.pagerduty_connection_id).",
				Optional: true,
			},
			"run_as": schema.StringAttribute{
				Description: "Identity triage sessions run under: 'organization', 'creator' or 'service_user' " +
					"(the service user named by run_as_service_user_id). Unset means 'organization'.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.OneOf("organization", "creator", "service_user")},
			},
			"run_as_service_user_id": schema.StringAttribute{
				Description: "ID of the service user triage sessions run as. Required when run_as is " +
					"'service_user' and must be unset otherwise.",
				Optional: true,
			},
			"slack_reply_access": schema.StringAttribute{
				Description: "Who may reply into triage sessions from Slack threads: 'devin_users', 'slack_users' " +
					"or 'external_slack_users'. Slack responders only; the server default is 'slack_users'.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), webhookRecomputeModifier{}},
				Validators:    []validator.String{stringvalidator.OneOf("devin_users", "slack_users", "external_slack_users")},
			},
			"teams_reply_access": schema.StringAttribute{
				Description: "Who may reply into triage sessions from Microsoft Teams threads: 'devin_users' or " +
					"'teams_users'. Teams responders only; the server default is 'teams_users'.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), webhookRecomputeModifier{}},
				Validators:    []validator.String{stringvalidator.OneOf("devin_users", "teams_users")},
			},
			"triage_issue_type": schema.StringAttribute{
				Description: "What the responder triages: 'alerts' (monitoring alerts, incidents and pages) or 'bugs' " +
					"(bug reports and support questions). Devin provisions a triage Board for the responder when set; " +
					"omitting it creates a legacy responder without one. Changing it replaces the responder, which " +
					"deletes its triage Board and the tickets on it.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("alerts", "bugs")},
			},
			"source": schema.StringAttribute{
				Description:   "Alert source derived from the triggers: slack, teams, pagerduty, webhook, linear or jira.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{webhookRecomputeModifier{}},
			},
			"source_channel_id": schema.StringAttribute{
				Description:   "Primary watched channel (Slack/Teams responders); empty for other sources.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{webhookRecomputeModifier{}},
			},
			"webhook_url": schema.StringAttribute{
				Description:   "Inbox URL alerting systems POST to; set only for webhook:incoming responders.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{webhookRecomputeModifier{}},
			},
			"webhook_secret": schema.StringAttribute{
				Description: "Secret alerting systems send in the X-Webhook-Secret header. Returned only when " +
					"minted, so it is preserved in state across updates.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{webhookRecomputeModifier{}},
			},
		},
	}
}

func (r *oncallResponderResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = oncallResponderIdentitySchema()
}

func (r *oncallResponderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *oncallResponderResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config oncallResponderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.RunAs.IsUnknown() || config.RunAsServiceUser.IsUnknown() {
		return
	}
	validateRunAs(config.runAsModel(), &resp.Diagnostics)
}

// runAsModel adapts the run_as attributes to the shared automation helpers.
func (m oncallResponderModel) runAsModel() automationModel {
	return automationModel{RunAs: m.RunAs, RunAsServiceUser: m.RunAsServiceUser}
}

func (r *oncallResponderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan oncallResponderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.OncallResponderCreateRequest{
		Name:                  plan.Name.ValueString(),
		Enabled:               boolPtrFrom(plan.Enabled),
		Metadata:              stringMapPtrFrom(ctx, plan.Metadata, &resp.Diagnostics),
		Triggers:              decodeJSONAttribute[[]api.OncallTriggerRequest](plan.Triggers, "triggers", &resp.Diagnostics),
		Tools:                 optionalGroupFrom[api.OncallTools](plan.Tools, "tools", &resp.Diagnostics),
		Limits:                optionalGroupFrom[api.AutomationLimits](plan.Limits, "limits", &resp.Diagnostics),
		SessionSettings:       optionalGroupFrom[api.OncallSessionSettingsInput](plan.SessionSettings, "session_settings", &resp.Diagnostics),
		Session:               optionalGroupFrom[api.OncallSessionConfig](plan.Session, "session", &resp.Diagnostics),
		Runbook:               optionalNullableString(plan.Runbook),
		TeamID:                optionalNullableString(plan.TeamID),
		PagerdutyConnectionID: optionalNullableString(plan.PagerdutyConnectionID),
	}
	validateRunAs(plan.runAsModel(), &resp.Diagnostics)
	body.RunAs = &api.OncallResponderCreateRequest_RunAs{}
	unionFromJSON(body.RunAs, runAsJSON(plan.runAsModel()), "run_as", &resp.Diagnostics)
	if !plan.SlackReplyAccess.IsNull() && !plan.SlackReplyAccess.IsUnknown() {
		slackReplyAccess := api.OncallResponderCreateRequestSlackReplyAccess(plan.SlackReplyAccess.ValueString())
		body.SlackReplyAccess = &slackReplyAccess
	}
	if !plan.TeamsReplyAccess.IsNull() && !plan.TeamsReplyAccess.IsUnknown() {
		body.TeamsReplyAccess = nullable.NewNullableWithValue(api.OncallResponderCreateRequestTeamsReplyAccess(plan.TeamsReplyAccess.ValueString()))
	}
	if !plan.TriageIssueType.IsNull() && !plan.TriageIssueType.IsUnknown() {
		body.TriageIssueType = nullable.NewNullableWithValue(api.OncallResponderCreateRequestTriageIssueType(plan.TriageIssueType.ValueString()))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.OncallResponderResponse
	if err := r.client.Post(ctx, orgOncallRespondersPath(plan.OrgID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to create Oncall responder", err.Error())
		return
	}

	plan.ResponderID = types.StringValue(result.ID)
	plan.applyComputed(&result, types.StringNull())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, oncallResponderIdentityModel{OrgID: plan.OrgID, ResponderID: plan.ResponderID}, &resp.Diagnostics)
}

func (r *oncallResponderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state oncallResponderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.OncallResponderResponse
	err := r.client.Get(ctx, orgOncallResponderPath(state.OrgID.ValueString(), state.ResponderID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, oncallResponderIdentityModel{OrgID: state.OrgID, ResponderID: state.ResponderID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read Oncall responder", err.Error())
		return
	}

	state.Name = types.StringValue(result.Name)
	state.Enabled = types.BoolValue(result.Enabled)
	priorMetadata := state.Metadata
	state.Metadata = stringMapFromPtr(ctx, result.Metadata, &resp.Diagnostics)
	if result.Metadata != nil && len(*result.Metadata) == 0 && priorMetadata.IsNull() {
		state.Metadata = types.MapNull(types.StringType)
	}
	state.PagerdutyConnectionID = stringFromNullable(result.PagerdutyConnectionID)
	state.RunAs, state.RunAsServiceUser = runAsFromResponse(result.RunAs, state.RunAs)
	state.applyComputed(&result, state.WebhookSecret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, oncallResponderIdentityModel{OrgID: state.OrgID, ResponderID: state.ResponderID}, &resp.Diagnostics)
}

func (r *oncallResponderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state oncallResponderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// PATCH is a merge patch: omitted members keep their stored value, so
	// changed JSON groups are null-filled to make the configured group
	// authoritative, and removed metadata keys are sent as explicit nulls.
	body := api.OncallResponderUpdateRequest{
		Name:     nullable.NewNullableWithValue(plan.Name.ValueString()),
		Enabled:  nullable.NewNullableWithValue(plan.Enabled.ValueBool()),
		Metadata: nullable.NewNullableWithValue(metadataPatch(ctx, plan.Metadata, state.Metadata, &resp.Diagnostics)),
	}
	if normalizedJSONChanged(ctx, plan.Triggers, state.Triggers, &resp.Diagnostics) {
		body.Triggers = nullable.NewNullableWithValue(decodeJSONAttribute[[]api.OncallTriggerRequest](plan.Triggers, "triggers", &resp.Diagnostics))
	}
	if normalizedJSONChanged(ctx, plan.Tools, state.Tools, &resp.Diagnostics) {
		body.Tools = nullableGroupFromJSON[api.OncallToolsPatch](plan.Tools, "tools", &resp.Diagnostics)
	}
	if normalizedJSONChanged(ctx, plan.Limits, state.Limits, &resp.Diagnostics) {
		body.Limits = nullableGroupFromJSON[api.OncallLimitsPatch](plan.Limits, "limits", &resp.Diagnostics)
	}
	if normalizedJSONChanged(ctx, plan.SessionSettings, state.SessionSettings, &resp.Diagnostics) {
		body.SessionSettings = nullableGroupFromJSON[api.OncallSessionSettingsPatch](plan.SessionSettings, "session_settings", &resp.Diagnostics)
	}
	if normalizedJSONChanged(ctx, plan.Session, state.Session, &resp.Diagnostics) {
		body.Session = nullableGroupFromJSON[api.OncallSessionConfig](plan.Session, "session", &resp.Diagnostics)
	}
	if !plan.Runbook.IsUnknown() && !plan.Runbook.Equal(state.Runbook) {
		body.Runbook = nullable.NewNullableWithValue(plan.Runbook.ValueString())
	}
	if !plan.TeamID.IsUnknown() && !plan.TeamID.IsNull() && !plan.TeamID.Equal(state.TeamID) {
		body.TeamID = nullable.NewNullableWithValue(plan.TeamID.ValueString())
	}
	if !plan.PagerdutyConnectionID.Equal(state.PagerdutyConnectionID) {
		if plan.PagerdutyConnectionID.IsNull() {
			body.PagerdutyConnectionID = nullable.NewNullNullable[string]()
		} else {
			body.PagerdutyConnectionID = nullable.NewNullableWithValue(plan.PagerdutyConnectionID.ValueString())
		}
	}
	validateRunAs(plan.runAsModel(), &resp.Diagnostics)
	var runAs api.OncallResponderUpdateRequest_RunAs
	unionFromJSON(&runAs, runAsJSON(plan.runAsModel()), "run_as", &resp.Diagnostics)
	body.RunAs = nullable.NewNullableWithValue(runAs)
	// Reply access is sent only when it changes: the API rejects each one on
	// responders of the other source, and the state holds the server default.
	if !plan.SlackReplyAccess.IsUnknown() && !plan.SlackReplyAccess.IsNull() && !plan.SlackReplyAccess.Equal(state.SlackReplyAccess) {
		body.SlackReplyAccess = nullable.NewNullableWithValue(api.OncallResponderUpdateRequestSlackReplyAccess(plan.SlackReplyAccess.ValueString()))
	}
	if !plan.TeamsReplyAccess.IsUnknown() && !plan.TeamsReplyAccess.IsNull() && !plan.TeamsReplyAccess.Equal(state.TeamsReplyAccess) {
		body.TeamsReplyAccess = nullable.NewNullableWithValue(api.OncallResponderUpdateRequestTeamsReplyAccess(plan.TeamsReplyAccess.ValueString()))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.OncallResponderResponse
	if err := r.client.Patch(ctx, orgOncallResponderPath(plan.OrgID.ValueString(), plan.ResponderID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update Oncall responder", err.Error())
		return
	}

	priorSecret := types.StringNull()
	if !state.WebhookURL.IsNull() {
		priorSecret = state.WebhookSecret
	}
	plan.applyComputed(&result, priorSecret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, oncallResponderIdentityModel{OrgID: plan.OrgID, ResponderID: plan.ResponderID}, &resp.Diagnostics)
}

func (r *oncallResponderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state oncallResponderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Delete(ctx, orgOncallResponderPath(state.OrgID.ValueString(), state.ResponderID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete Oncall responder", err.Error())
	}
}

func (r *oncallResponderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "responder_id")
}

// applyComputed refreshes the server-derived attributes. The webhook secret
// is only returned when minted, so priorSecret is carried forward otherwise.
func (m *oncallResponderModel) applyComputed(result *api.OncallResponderResponse, priorSecret types.String) {
	// The server trims the runbook.
	if m.Runbook.IsNull() || m.Runbook.IsUnknown() || strings.TrimSpace(m.Runbook.ValueString()) != result.Runbook {
		m.Runbook = types.StringValue(result.Runbook)
	}
	m.TeamID = stringFromNullable(result.TeamID)
	m.Source = types.StringNull()
	if result.Source != nil {
		m.Source = types.StringValue(string(*result.Source))
	}
	m.SourceChannelID = types.StringValue(result.SourceChannelID)
	m.SlackReplyAccess, m.TeamsReplyAccess = types.StringNull(), types.StringNull()
	if result.SlackReplyAccess != nil {
		m.SlackReplyAccess = types.StringValue(string(*result.SlackReplyAccess))
	}
	if result.TeamsReplyAccess != nil {
		m.TeamsReplyAccess = types.StringValue(string(*result.TeamsReplyAccess))
	}
	m.TriageIssueType = types.StringNull()
	if result.TriageIssueType.IsSpecified() && !result.TriageIssueType.IsNull() {
		m.TriageIssueType = types.StringValue(string(result.TriageIssueType.MustGet()))
	}
	m.WebhookURL, m.WebhookSecret = types.StringNull(), types.StringNull()
	if result.Webhook.IsSpecified() && !result.Webhook.IsNull() {
		webhook := result.Webhook.MustGet()
		m.WebhookURL = types.StringValue(webhook.URL)
		m.WebhookSecret = stringFromNullable(webhook.Secret)
		if m.WebhookSecret.IsNull() {
			m.WebhookSecret = priorSecret
		}
	}
}

func optionalNullableString(value types.String) nullable.Nullable[string] {
	var result nullable.Nullable[string]
	if !value.IsNull() && !value.IsUnknown() {
		result.Set(value.ValueString())
	}
	return result
}
