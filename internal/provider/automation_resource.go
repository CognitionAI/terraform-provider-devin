package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &automationResource{}
var _ resource.ResourceWithImportState = &automationResource{}
var _ resource.ResourceWithIdentity = &automationResource{}
var _ resource.ResourceWithValidateConfig = &automationResource{}

type automationResource struct {
	client *Client
}

// The trigger/action/notification/limits/concurrency/tools/session-settings
// groups are
// deep discriminated unions in the API, so they are modeled as JSON-encoded
// attributes and treated as config-authoritative: the configured value is
// what Terraform stores and diffs, and Read does not overwrite them with the
// server's normalized/enriched echo (which adds server-minted fields such as
// trigger_id). Scalars (name, enabled, metadata, run_as) are refreshed on
// Read.
type automationModel struct {
	AutomationID     types.String         `tfsdk:"automation_id"`
	OrgID            types.String         `tfsdk:"org_id"`
	Name             types.String         `tfsdk:"name"`
	Enabled          types.Bool           `tfsdk:"enabled"`
	Metadata         types.Map            `tfsdk:"metadata"`
	Triggers         jsontypes.Normalized `tfsdk:"triggers"`
	Actions          jsontypes.Normalized `tfsdk:"actions"`
	Notifications    jsontypes.Normalized `tfsdk:"notifications"`
	Limits           jsontypes.Normalized `tfsdk:"limits"`
	Concurrency      jsontypes.Normalized `tfsdk:"concurrency"`
	Tools            jsontypes.Normalized `tfsdk:"tools"`
	SessionSettings  jsontypes.Normalized `tfsdk:"session_settings"`
	RunAs            types.String         `tfsdk:"run_as"`
	RunAsServiceUser types.String         `tfsdk:"run_as_service_user_id"`
	SlackReplyAccess types.String         `tfsdk:"slack_reply_access"`
	TeamsReplyAccess types.String         `tfsdk:"teams_reply_access"`
	WebhookURL       types.String         `tfsdk:"webhook_url"`
	WebhookSecret    types.String         `tfsdk:"webhook_secret"`
}

func NewAutomationResource() resource.Resource {
	return &automationResource{}
}

func (r *automationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_automation"
}

func (r *automationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Devin automation within an organization. Automations run Devin in " +
			"response to events (GitHub/GitLab activity, Slack messages, Jira/Linear updates, " +
			"schedules, incoming webhooks). The triggers/actions/notifications/limits/" +
			"concurrency/tools/" +
			"session_settings groups are JSON-encoded (use jsonencode(...)); their configured " +
			"values are authoritative and out-of-band edits to them are not detected as drift. " +
			"The organization's automation event schemas endpoint " +
			"(GET /v3/organizations/{org_id}/automations/schemas) documents the supported trigger " +
			"event types, condition fields, and reply verbs.",
		Attributes: map[string]schema.Attribute{
			"automation_id": schema.StringAttribute{
				Description: "Automation ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns this automation.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Automation name.",
				Required:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the automation fires on matching events. Defaults to true.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"metadata": schema.MapAttribute{
				ElementType: types.StringType,
				Description: "Org-visible key/value labels for organizing/filtering automations. " +
					"At most 16 pairs; keys at most 32 chars; values at most 128.",
				Optional: true,
			},
			"triggers": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded array of triggers (use jsonencode(...)). Each trigger is an " +
					"object with event_type (e.g. 'github:pull_request'), optional conditions (two-level " +
					"any/all envelope of {field, operator, value} conditions). Slack message triggers require " +
					"a channel restriction or completed filter in every group; is_thread_reply alone is insufficient. " +
					"For other events, null conditions match every event. " +
					"and optional replies (array of {type} where type is notify_thread, attach_thread, or " +
					"post_response). The automation fires when any trigger matches; at most one " +
					"webhook:incoming trigger.",
				Required: true,
			},
			"actions": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded array of actions (use jsonencode(...)), e.g. " +
					"[{type = \"start_session\", prompt = \"...\"}]. Action types: start_session, " +
					"message_session, monitor_session. Other flag-gated types " +
					"pass through as sent; on update the server keeps their members that the " +
					"configuration omits (the provider warns). Non-empty; at most one " +
					"start_session; monitor_session must be the only action. Members modeled by " +
					"the API as non-nullable optional scalars can be changed but cannot be cleared " +
					"through PATCH. For tagging-enforced enterprises, start_session actions must " +
					"declare session.tags with exactly one allowed tag — the configured action is " +
					"authoritative, so tags set outside Terraform do not survive an update.",
				Required: true,
			},
			"notifications": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded notifications object (use jsonencode(...)), e.g. " +
					"{email = {when = \"always\"}}.",
				Optional: true,
			},
			"limits": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded limits object (use jsonencode(...)): max_acu_limit (1-1000) " +
					"and/or invocations ({max_per_window >= 1, window_seconds >= 60}).",
				Optional: true,
			},
			"concurrency": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded concurrency object (use jsonencode(...)): " +
					"max_concurrent_runs (>= 1; further triggered events wait in the automation's " +
					"queue) and/or max_queue_depth (>= 0; further events are dropped). Null members " +
					"mean unlimited.",
				Optional: true,
			},
			"tools": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded tools object (use jsonencode(...)), e.g. MCP servers and " +
					"Linear tooling for spawned sessions. Members modeled by the API as non-nullable " +
					"optional scalars can be changed but cannot be cleared through PATCH.",
				Optional: true,
			},
			"session_settings": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "JSON-encoded settings object applied to sessions this automation spawns " +
					"(use jsonencode(...)). Members modeled by the API as non-nullable optional " +
					"scalars can be changed but cannot be cleared through PATCH.",
				Optional: true,
			},
			"run_as": schema.StringAttribute{
				Description: "Identity the spawned sessions run under: 'organization', 'creator' " +
					"(personal automation, visible only to the creator and org admins; rejected for " +
					"service-user-created automations) or 'service_user' (the service user named by " +
					"run_as_service_user_id; requires permission to manage that service user). The " +
					"API requires an explicit choice on create, so the provider sends 'organization' " +
					"when this is not set.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("organization", "creator", "service_user"),
				},
			},
			"run_as_service_user_id": schema.StringAttribute{
				Description: "ID of the service user the spawned sessions run as. Required when run_as " +
					"is 'service_user' and must be unset otherwise.",
				Optional: true,
			},
			"slack_reply_access": schema.StringAttribute{
				Description: "Who may reply into existing sessions from Slack threads: 'devin_users' " +
					"(linked Devin accounts only), 'slack_users' (anyone in the organization's connected " +
					"Slack workspaces), or 'external_slack_users' (also Slack Connect users). Does not " +
					"affect who can trigger the automation. Defaults to 'slack_users'.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("slack_users"),
				Validators: []validator.String{
					stringvalidator.OneOf("devin_users", "slack_users", "external_slack_users"),
				},
			},
			"teams_reply_access": schema.StringAttribute{
				Description: "Who may reply into existing sessions from Microsoft Teams threads: 'devin_users' " +
					"(linked Devin accounts only) or 'teams_users' (anyone in the organization's connected " +
					"Microsoft tenant). Does not affect who can trigger the automation. Defaults to 'teams_users'.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("teams_users"),
				Validators: []validator.String{
					stringvalidator.OneOf("devin_users", "teams_users"),
				},
			},
			"webhook_url": schema.StringAttribute{
				Description: "Inbox URL external systems POST to; set only when the automation has a " +
					"webhook:incoming trigger.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					webhookRecomputeModifier{},
				},
			},
			"webhook_secret": schema.StringAttribute{
				Description: "Secret external systems send in the X-Webhook-Secret header. Minted when " +
					"the webhook:incoming trigger is first added and never retrievable again, so it is " +
					"preserved in state across updates.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					webhookRecomputeModifier{},
				},
			},
		},
	}
}

func (r *automationResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = automationIdentitySchema()
}

func (r *automationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *automationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan automationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.AutomationCreateRequest{
		Name:            plan.Name.ValueString(),
		Enabled:         boolPtrFrom(plan.Enabled),
		Metadata:        stringMapPtrFrom(ctx, plan.Metadata, &resp.Diagnostics),
		Triggers:        decodeJSONAttribute[[]api.AutomationTriggerRequestInput](plan.Triggers, "triggers", &resp.Diagnostics),
		Actions:         decodeJSONAttribute[[]api.AutomationCreateRequest_Actions_Item](plan.Actions, "actions", &resp.Diagnostics),
		Notifications:   optionalGroupFrom[api.AutomationNotifications](plan.Notifications, "notifications", &resp.Diagnostics),
		Limits:          optionalGroupFrom[api.AutomationLimits](plan.Limits, "limits", &resp.Diagnostics),
		Concurrency:     optionalGroupFrom[api.AutomationConcurrency](plan.Concurrency, "concurrency", &resp.Diagnostics),
		Tools:           optionalGroupFrom[api.AutomationTools](plan.Tools, "tools", &resp.Diagnostics),
		SessionSettings: optionalGroupFrom[api.AutomationSessionSettingsInput](plan.SessionSettings, "session_settings", &resp.Diagnostics),
	}
	// The API requires an explicit run-as choice on create; an unset
	// attribute means the organization identity.
	validateRunAs(plan, &resp.Diagnostics)
	unionFromJSON(&body.RunAs, runAsJSON(plan), "run_as", &resp.Diagnostics)
	if !plan.SlackReplyAccess.IsNull() && !plan.SlackReplyAccess.IsUnknown() {
		slackReplyAccess := api.AutomationCreateRequestSlackReplyAccess(plan.SlackReplyAccess.ValueString())
		body.SlackReplyAccess = &slackReplyAccess
	}
	if !plan.TeamsReplyAccess.IsNull() && !plan.TeamsReplyAccess.IsUnknown() {
		teamsReplyAccess := api.AutomationCreateRequestTeamsReplyAccess(plan.TeamsReplyAccess.ValueString())
		body.TeamsReplyAccess = &teamsReplyAccess
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.AutomationResponse
	err := r.client.Post(ctx, orgAutomationsPath(plan.OrgID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create automation", err.Error())
		return
	}

	plan.AutomationID = types.StringValue(result.AutomationID)
	plan.WebhookURL, plan.WebhookSecret = automationWebhook(&result, types.StringNull())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, automationIdentityModel{OrgID: plan.OrgID, AutomationID: plan.AutomationID}, &resp.Diagnostics)
}

func (r *automationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state automationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.AutomationResponse
	err := r.client.Get(ctx, orgAutomationPath(state.OrgID.ValueString(), state.AutomationID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, automationIdentityModel{OrgID: state.OrgID, AutomationID: state.AutomationID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read automation", err.Error())
		return
	}

	state.Name = types.StringValue(result.Name)
	state.Enabled = types.BoolValue(result.Enabled)
	priorMetadata := state.Metadata
	state.Metadata = stringMapFromPtr(ctx, result.Metadata, &resp.Diagnostics)
	// The API normalizes both omitted metadata and an explicitly empty map to
	// an empty object. Preserve a null prior state for omitted configuration.
	if result.Metadata != nil && len(*result.Metadata) == 0 && priorMetadata.IsNull() {
		state.Metadata = types.MapNull(types.StringType)
	}
	state.RunAs, state.RunAsServiceUser = automationRunAsFromResponse(&result, state.RunAs)
	state.SlackReplyAccess = automationSlackReplyAccessFromResponse(&result)
	state.TeamsReplyAccess = automationTeamsReplyAccessFromResponse(&result)
	state.WebhookURL, state.WebhookSecret = automationWebhook(&result, state.WebhookSecret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, automationIdentityModel{OrgID: state.OrgID, AutomationID: state.AutomationID}, &resp.Diagnostics)
}

func (r *automationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state automationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Metadata merges per key with null values deleting, so removed keys must
	// be sent as explicit nulls rather than just omitted. Under the legacy
	// replace_groups semantics (the server-side recursive-merge kill switch),
	// null values are rejected and the sent map replaces the stored one
	// wholesale, so deletions happen by omission instead. The active mode is
	// advertised by the schemas endpoint; only consult it when a deletion is
	// actually pending.
	metadata := metadataPatch(ctx, plan.Metadata, state.Metadata, &resp.Diagnostics)
	if metadataHasDeletions(metadata) && r.updateSemantics(ctx, plan.OrgID.ValueString()) == "replace_groups" {
		metadata = metadataWithoutDeletions(metadata)
	}

	// PATCH merges member-wise: omitted groups keep their stored value. Send
	// only changed JSON groups, filling nullable members with null and
	// non-nullable lists with empty arrays so configured groups are authoritative.
	body := api.AutomationUpdateRequest{
		Name:     nullable.NewNullableWithValue(plan.Name.ValueString()),
		Enabled:  nullable.NewNullableWithValue(plan.Enabled.ValueBool()),
		Metadata: nullable.NewNullableWithValue(metadata),
	}
	if normalizedJSONChanged(ctx, plan.Triggers, state.Triggers, &resp.Diagnostics) {
		body.Triggers = nullable.NewNullableWithValue(decodeAndNullFillTriggers(plan.Triggers, &resp.Diagnostics))
	}
	if normalizedJSONChanged(ctx, plan.Actions, state.Actions, &resp.Diagnostics) {
		body.Actions = nullable.NewNullableWithValue(decodeAndNullFillActions(plan.Actions, &resp.Diagnostics))
	}
	if normalizedJSONChanged(ctx, plan.Notifications, state.Notifications, &resp.Diagnostics) {
		body.Notifications = nullableGroupFromJSON[api.AutomationNotifications](plan.Notifications, "notifications", &resp.Diagnostics)
	}
	if normalizedJSONChanged(ctx, plan.Limits, state.Limits, &resp.Diagnostics) {
		body.Limits = nullableGroupFromJSON[api.AutomationLimits](plan.Limits, "limits", &resp.Diagnostics)
	}
	if normalizedJSONChanged(ctx, plan.Concurrency, state.Concurrency, &resp.Diagnostics) {
		body.Concurrency = nullableGroupFromJSON[api.AutomationConcurrency](plan.Concurrency, "concurrency", &resp.Diagnostics)
	}
	if normalizedJSONChanged(ctx, plan.Tools, state.Tools, &resp.Diagnostics) {
		body.Tools = nullableGroupFromJSON[api.AutomationTools](plan.Tools, "tools", &resp.Diagnostics)
	}
	if normalizedJSONChanged(ctx, plan.SessionSettings, state.SessionSettings, &resp.Diagnostics) {
		body.SessionSettings = nullableGroupFromJSON[api.AutomationSessionSettingsInput](plan.SessionSettings, "session_settings", &resp.Diagnostics)
	}
	validateRunAs(plan, &resp.Diagnostics)
	if plan.RunAs.IsNull() {
		// Explicit null resets to the default organization identity.
		body.RunAs = nullable.NewNullNullable[api.AutomationUpdateRequest_RunAs]()
	} else {
		var runAs api.AutomationUpdateRequest_RunAs
		unionFromJSON(&runAs, runAsJSON(plan), "run_as", &resp.Diagnostics)
		body.RunAs = nullable.NewNullableWithValue(runAs)
	}
	if plan.SlackReplyAccess.IsNull() {
		body.SlackReplyAccess = nullable.NewNullNullable[api.AutomationUpdateRequestSlackReplyAccess]()
	} else if !plan.SlackReplyAccess.IsUnknown() {
		body.SlackReplyAccess = nullable.NewNullableWithValue(api.AutomationUpdateRequestSlackReplyAccess(plan.SlackReplyAccess.ValueString()))
	}
	if plan.TeamsReplyAccess.IsNull() {
		body.TeamsReplyAccess = nullable.NewNullNullable[api.AutomationUpdateRequestTeamsReplyAccess]()
	} else if !plan.TeamsReplyAccess.IsUnknown() {
		body.TeamsReplyAccess = nullable.NewNullableWithValue(api.AutomationUpdateRequestTeamsReplyAccess(plan.TeamsReplyAccess.ValueString()))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.AutomationResponse
	err := r.client.Patch(ctx, orgAutomationPath(plan.OrgID.ValueString(), plan.AutomationID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update automation", err.Error())
		return
	}

	priorSecret := types.StringNull()
	if !state.WebhookURL.IsNull() {
		priorSecret = state.WebhookSecret
	}
	plan.WebhookURL, plan.WebhookSecret = automationWebhook(&result, priorSecret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, automationIdentityModel{OrgID: plan.OrgID, AutomationID: plan.AutomationID}, &resp.Diagnostics)
}

func (r *automationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state automationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, orgAutomationPath(state.OrgID.ValueString(), state.AutomationID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete automation", err.Error())
	}
}

func (r *automationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "automation_id")
}

// decodeJSONAttribute decodes a JSON-encoded attribute into the generated
// request type. Unknown keys in the typed layers are rejected locally so
// config typos surface as errors rather than being dropped from the request;
// union-typed layers (conditions, action payloads) pass through as raw JSON
// for the API to validate.
func decodeJSONAttribute[T any](value jsontypes.Normalized, attribute string, diags *diag.Diagnostics) T {
	var out T
	decoder := json.NewDecoder(bytes.NewReader([]byte(value.ValueString())))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		diags.AddError("Invalid "+attribute, err.Error())
	}
	return out
}

func normalizedJSONChanged(ctx context.Context, plan, state jsontypes.Normalized, diags *diag.Diagnostics) bool {
	if plan.IsNull() || plan.IsUnknown() || state.IsNull() || state.IsUnknown() {
		return !plan.IsNull() || !state.IsNull()
	}
	equal, semanticDiags := plan.StringSemanticEquals(ctx, state)
	diags.Append(semanticDiags...)
	return !equal
}

// optionalGroupFrom decodes an optional JSON-encoded group attribute, leaving
// the request field unspecified (omitted) when the value is null. A literal
// JSON null payload (jsonencode(null)) also means unspecified, matching the
// update path's clear-with-null handling.
func optionalGroupFrom[T any](value jsontypes.Normalized, attribute string, diags *diag.Diagnostics) nullable.Nullable[T] {
	var result nullable.Nullable[T]
	if value.IsNull() || value.IsUnknown() {
		return result
	}
	if strings.TrimSpace(value.ValueString()) == "null" {
		return result
	}
	result.Set(decodeJSONAttribute[T](value, attribute, diags))
	return result
}

func nullableGroupFromJSON[T any](value jsontypes.Normalized, attribute string, diags *diag.Diagnostics) nullable.Nullable[T] {
	if value.IsNull() || value.IsUnknown() {
		return nullable.NewNullNullable[T]()
	}
	if strings.TrimSpace(value.ValueString()) == "null" {
		return nullable.NewNullNullable[T]()
	}
	return nullable.NewNullableWithValue(decodeAndNullFillJSON[T](value, attribute, diags))
}

func decodeAndNullFillJSON[T any](value jsontypes.Normalized, attribute string, diags *diag.Diagnostics) T {
	var out T
	raw := []byte(value.ValueString())
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		diags.AddError("Invalid "+attribute, err.Error())
		return out
	}
	filled, err := nullFillJSONMembers(raw, reflect.TypeOf(out))
	if err != nil {
		diags.AddError("Invalid "+attribute, err.Error())
		return out
	}
	if err := json.Unmarshal(filled, &out); err != nil {
		diags.AddError("Invalid "+attribute, err.Error())
	}
	return out
}

func decodeAndNullFillActions(value jsontypes.Normalized, diags *diag.Diagnostics) []api.AutomationUpdateRequest_Actions_Item {
	var out []api.AutomationUpdateRequest_Actions_Item
	raw := []byte(value.ValueString())
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		diags.AddError("Invalid actions", err.Error())
		return out
	}
	filled, skipped, err := nullFillActions(raw)
	if err != nil {
		diags.AddError("Invalid actions", err.Error())
		return out
	}
	warned := map[string]bool{}
	for _, actionType := range skipped {
		if warned[actionType] {
			continue
		}
		warned[actionType] = true
		diags.AddWarning(
			"Action type without a published update schema",
			fmt.Sprintf("Action type %q is not in the published API schema. "+
				"On update, the server keeps the stored values of members "+
				"omitted from the configuration instead of clearing them, so "+
				"removals inside this action are not applied.", actionType),
		)
	}
	if err := json.Unmarshal(filled, &out); err != nil {
		diags.AddError("Invalid actions", err.Error())
	}
	return out
}

func decodeAndNullFillTriggers(value jsontypes.Normalized, diags *diag.Diagnostics) []api.AutomationTriggerRequestInput {
	var out []api.AutomationTriggerRequestInput
	raw := []byte(value.ValueString())
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		diags.AddError("Invalid triggers", err.Error())
		return out
	}
	var triggers []map[string]any
	if err := json.Unmarshal(raw, &triggers); err != nil {
		diags.AddError("Invalid triggers", err.Error())
		return out
	}
	for i, trigger := range triggers {
		if trigger == nil {
			diags.AddError("Invalid triggers", fmt.Sprintf("trigger %d must be an object, not null", i))
			return out
		}
		filled, err := nullFillObject(trigger, reflect.TypeOf(api.AutomationTriggerRequestInput{}))
		if err != nil {
			diags.AddError("Invalid triggers", fmt.Sprintf("trigger %d: %v", i, err))
			return out
		}
		triggers[i] = filled
	}
	filled, err := json.Marshal(triggers)
	if err != nil {
		diags.AddError("Invalid triggers", err.Error())
		return out
	}
	if err := json.Unmarshal(filled, &out); err != nil {
		diags.AddError("Invalid triggers", err.Error())
	}
	return out
}

// actionUpdateSchemas maps the published action types to their generated
// update models for null-filling. Types that the server gates behind a
// feature flag (e.g. triage_session, remediate_finding) are absent from the
// published spec, so no generated model exists to fill against; they pass
// through verbatim and updates emit a warning (see decodeAndNullFillActions).
// A unit test checks this map against every *ActionUpdate model in
// models.gen.go, so a type that enters the published spec must get an entry.
var actionUpdateSchemas = map[string]reflect.Type{
	"start_session":   reflect.TypeOf(api.AutomationStartSessionActionUpdate{}),
	"message_session": reflect.TypeOf(api.AutomationMessageSessionActionUpdate{}),
	"monitor_session": reflect.TypeOf(api.AutomationMonitorSessionActionUpdate{}),
}

func nullFillActions(raw []byte) ([]byte, []string, error) {
	var skipped []string
	var actions []map[string]any
	if err := json.Unmarshal(raw, &actions); err != nil {
		return nil, nil, err
	}
	for i, action := range actions {
		if action == nil {
			return nil, nil, fmt.Errorf("action %d must be an object, not null", i)
		}
		actionType, ok := action["type"].(string)
		if !ok {
			return nil, nil, fmt.Errorf("action %d is missing string type", i)
		}
		schema, known := actionUpdateSchemas[actionType]
		if !known {
			// Flag-gated action types are forwarded verbatim: the server
			// validates them, but its member-wise merge keeps the stored
			// values of omitted members instead of clearing them.
			skipped = append(skipped, actionType)
			continue
		}
		_, hadTarget := action["target_devin_id"]
		autoCreateValue, hadAutoCreate := action["auto_create"]
		autoCreate, _ := autoCreateValue.(bool)
		filled, err := nullFillObject(action, schema)
		if err != nil {
			return nil, nil, fmt.Errorf("action %d: %w", i, err)
		}
		// The executor owns target_devin_id when auto_create is on (it binds
		// the long-running session it creates on first fire), so an omitted
		// member must stay omitted rather than being null-filled: an explicit
		// null would clear the binding until the next invocation re-fills it.
		// An omitted auto_create is merged from the stored action server-side
		// and may be true, so an omitted target must also stay omitted then.
		if actionType == "message_session" && !hadTarget && (autoCreate || !hadAutoCreate) {
			delete(filled, "target_devin_id")
		}
		actions[i] = filled
	}
	data, err := json.Marshal(actions)
	return data, skipped, err
}

func nullFillJSONMembers(raw []byte, schema reflect.Type) ([]byte, error) {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	filled, err := nullFillObject(object, schema)
	if err != nil {
		return nil, err
	}
	return json.Marshal(filled)
}

func nullFillObject(object map[string]any, schema reflect.Type) (map[string]any, error) {
	if object == nil {
		return nil, nil
	}
	schema = jsonSchemaType(schema)
	if schema.Kind() != reflect.Struct {
		return object, nil
	}
	if !hasExportedFields(schema) {
		// oapi-codegen represents discriminated unions and arbitrary JSON
		// values as opaque wrappers with only an unexported union field.
		return object, nil
	}
	for fieldIndex := 0; fieldIndex < schema.NumField(); fieldIndex++ {
		field := schema.Field(fieldIndex)
		if !field.IsExported() {
			continue
		}
		name := jsonFieldName(field)
		if name == "" || name == "-" || readOnlyAutomationJSONField(schema, name) {
			continue
		}
		value, present := object[name]
		if !present {
			if keepOmittedAutomationJSONField(schema, name) {
				continue
			}
			switch {
			case isNullableType(field.Type):
				object[name] = nil
			case isPointerToSlice(field.Type):
				object[name] = []any{}
			case isPointerToFillableStruct(field.Type):
				filled, err := nullFillObject(map[string]any{}, field.Type)
				if err != nil {
					return nil, err
				}
				object[name] = filled
			}
			continue
		}
		fieldType := jsonSchemaType(field.Type)
		switch fieldType.Kind() {
		case reflect.Struct:
			if nested, ok := value.(map[string]any); ok {
				filled, err := nullFillObject(nested, fieldType)
				if err != nil {
					return nil, err
				}
				object[name] = filled
			}
		case reflect.Slice, reflect.Array:
			if nested, ok := value.([]any); ok {
				elementType := fieldType.Elem()
				for i, element := range nested {
					if nestedObject, ok := element.(map[string]any); ok {
						filled, err := nullFillObject(nestedObject, elementType)
						if err != nil {
							return nil, err
						}
						nested[i] = filled
					}
				}
				object[name] = nested
			}
		}
	}
	return object, nil
}

func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	return strings.Split(tag, ",")[0]
}

func jsonSchemaType(fieldType reflect.Type) reflect.Type {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	// oapi-codegen/nullable.Nullable[T] is map[bool]T. Its JSON value has
	// the shape of T, not the implementation map.
	if fieldType.Kind() == reflect.Map && fieldType.Key().Kind() == reflect.Bool {
		fieldType = fieldType.Elem()
	}
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	return fieldType
}

func isNullableType(fieldType reflect.Type) bool {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	return fieldType.Kind() == reflect.Map &&
		fieldType.Key().Kind() == reflect.Bool
}

func isPointerToSlice(fieldType reflect.Type) bool {
	return fieldType.Kind() == reflect.Pointer && fieldType.Elem().Kind() == reflect.Slice
}

func isPointerToFillableStruct(fieldType reflect.Type) bool {
	return fieldType.Kind() == reflect.Pointer &&
		fieldType.Elem().Kind() == reflect.Struct &&
		hasExportedFields(fieldType.Elem())
}

func hasExportedFields(schema reflect.Type) bool {
	for fieldIndex := 0; fieldIndex < schema.NumField(); fieldIndex++ {
		if schema.Field(fieldIndex).IsExported() {
			return true
		}
	}
	return false
}

// Members that are required when creating or adding an action reject an
// explicit null on a merge-semantics update (the API carries the stored
// value only when they are omitted), so an absent member must stay absent
// rather than being null-filled.
func keepOmittedAutomationJSONField(schema reflect.Type, name string) bool {
	switch jsonSchemaType(schema) {
	case reflect.TypeOf(api.AutomationStartSessionActionUpdate{}),
		reflect.TypeOf(api.AutomationMessageSessionActionUpdate{}):
		return name == "prompt"
	case reflect.TypeOf(api.AutomationMonitorSessionActionUpdate{}):
		return name == "setup_prompt" || name == "slack_monitor_config"
	}
	return false
}

// Session config input exposes repos and playbook_id for compatibility, but
// the server derives them from the prompt and rejects them when sent.
func readOnlyAutomationJSONField(schema reflect.Type, name string) bool {
	schema = jsonSchemaType(schema)
	return schema == reflect.TypeOf(api.AutomationSessionConfigInput{}) &&
		(name == "repos" || name == "playbook_id")
}

// unionFromJSON stores raw JSON into a generated union type via its
// UnmarshalJSON, which keeps the bytes verbatim for the request body.
func unionFromJSON(target json.Unmarshaler, raw string, attribute string, diags *diag.Diagnostics) {
	if err := target.UnmarshalJSON([]byte(raw)); err != nil {
		diags.AddError("Invalid "+attribute, err.Error())
	}
}

// ValidateConfig mirrors the API's run_as rules so invalid combinations
// fail at plan time when both values are known; Create and Update re-check
// them at apply time for values that were unknown during planning.
func (r *automationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config automationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.RunAs.IsUnknown() || config.RunAsServiceUser.IsUnknown() {
		return
	}
	validateRunAs(config, &resp.Diagnostics)
}

// validateRunAs enforces the run_as / run_as_service_user_id pairing: a
// non-empty run_as_service_user_id is required with 'service_user' and
// rejected with any other (or an unset) run_as.
func validateRunAs(m automationModel, diags *diag.Diagnostics) {
	isServiceUser := m.RunAs.ValueString() == "service_user"
	hasServiceUser := !m.RunAsServiceUser.IsNull() && m.RunAsServiceUser.ValueString() != ""
	switch {
	case isServiceUser && !hasServiceUser:
		diags.AddAttributeError(path.Root("run_as_service_user_id"), "Missing service user",
			"run_as_service_user_id is required when run_as is 'service_user'.")
	case !isServiceUser && !m.RunAsServiceUser.IsNull():
		diags.AddAttributeError(path.Root("run_as_service_user_id"), "Unexpected service user",
			"run_as_service_user_id may only be set when run_as is 'service_user'.")
	}
}

// runAsJSON builds the run_as union body from the plan. An unset run_as
// means the organization identity.
func runAsJSON(plan automationModel) string {
	runAs := "organization"
	if !plan.RunAs.IsNull() {
		runAs = plan.RunAs.ValueString()
	}
	body := map[string]string{"type": runAs}
	if runAs == "service_user" {
		body["service_user_id"] = plan.RunAsServiceUser.ValueString()
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// metadataPatch builds the PATCH metadata map: every planned key with its
// value, plus an explicit null for each key present in prior state but
// removed from the plan.
func metadataPatch(ctx context.Context, plan, state types.Map, diags *diag.Diagnostics) map[string]*string {
	planned := stringMapFrom(ctx, plan, diags)
	prior := stringMapFrom(ctx, state, diags)
	patch := make(map[string]*string, len(planned)+len(prior))
	for key, value := range planned {
		v := value
		patch[key] = &v
	}
	for key := range prior {
		if _, ok := planned[key]; !ok {
			patch[key] = nil
		}
	}
	return patch
}

func metadataHasDeletions(patch map[string]*string) bool {
	for _, value := range patch {
		if value == nil {
			return true
		}
	}
	return false
}

func metadataWithoutDeletions(patch map[string]*string) map[string]*string {
	out := make(map[string]*string, len(patch))
	for key, value := range patch {
		if value != nil {
			out[key] = value
		}
	}
	return out
}

// updateSemantics reports the org's active PATCH semantics as advertised by
// the schemas endpoint: "merge_patch" (default) or "replace_groups" (the
// server-side recursive-merge kill switch). Fails open to the default on any
// error — the subsequent PATCH surfaces real problems.
func (r *automationResource) updateSemantics(ctx context.Context, orgID string) string {
	var schemas struct {
		UpdateSemantics string `json:"update_semantics"`
	}
	if err := r.client.Get(ctx, orgAutomationsPath(orgID)+"/schemas", &schemas); err != nil || schemas.UpdateSemantics == "" {
		return "merge_patch"
	}
	return schemas.UpdateSemantics
}

func stringMapFrom(ctx context.Context, value types.Map, diags *diag.Diagnostics) map[string]string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	out := map[string]string{}
	diags.Append(value.ElementsAs(ctx, &out, false)...)
	return out
}

func stringMapPtrFrom(ctx context.Context, value types.Map, diags *diag.Diagnostics) *map[string]string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	out := stringMapFrom(ctx, value, diags)
	return &out
}

func stringMapFromPtr(ctx context.Context, value *map[string]string, diags *diag.Diagnostics) types.Map {
	if value == nil {
		return types.MapNull(types.StringType)
	}
	mapped, d := types.MapValueFrom(ctx, types.StringType, *value)
	diags.Append(d...)
	return mapped
}

// webhookRecomputeModifier marks a computed webhook attribute as unknown when
// an update changes the triggers. The webhook inbox exists only while a
// webhook:incoming trigger exists, so a trigger edit can change or clear the
// attribute. Without this, the plan keeps the prior value, and an apply that
// removes the trigger fails with an inconsistent-result error.
type webhookRecomputeModifier struct{}

func (webhookRecomputeModifier) Description(context.Context) string {
	return "Recomputed when the triggers change."
}

func (webhookRecomputeModifier) MarkdownDescription(context.Context) string {
	return "Recomputed when the triggers change."
}

func (webhookRecomputeModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		// Create and destroy plans need no recompute marker.
		return
	}
	var planTriggers, stateTriggers jsontypes.Normalized
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("triggers"), &planTriggers)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("triggers"), &stateTriggers)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if normalizedJSONChanged(ctx, planTriggers, stateTriggers, &resp.Diagnostics) {
		resp.PlanValue = types.StringUnknown()
	}
}

// automationRunAsFromResponse extracts the run-as identity and, for
// 'service_user', the service user id from the response union; a missing or
// unrecognized value means the organization default. The type is passed
// through normalizeRunAsState against the prior run_as state.
func automationRunAsFromResponse(result *api.AutomationResponse, prior types.String) (types.String, types.String) {
	var runAs struct {
		Type          string `json:"type"`
		ServiceUserID string `json:"service_user_id"`
	}
	if result.RunAs != nil {
		if raw, err := json.Marshal(result.RunAs); err == nil {
			_ = json.Unmarshal(raw, &runAs)
		}
	}
	if runAs.Type == "" {
		runAs.Type = "organization"
	}
	serviceUser := types.StringNull()
	if runAs.Type == "service_user" {
		serviceUser = types.StringValue(runAs.ServiceUserID)
	}
	return normalizeRunAsState(runAs.Type, prior), serviceUser
}

// automationSlackReplyAccessFromResponse maps an omitted policy to the API
// default.
func automationSlackReplyAccessFromResponse(result *api.AutomationResponse) types.String {
	if result.SlackReplyAccess == nil {
		return types.StringValue(string(api.AutomationResponseSlackReplyAccessSlackUsers))
	}
	return types.StringValue(string(*result.SlackReplyAccess))
}

// automationTeamsReplyAccessFromResponse maps an omitted policy to the API
// default.
func automationTeamsReplyAccessFromResponse(result *api.AutomationResponse) types.String {
	if result.TeamsReplyAccess == nil {
		return types.StringValue(string(api.AutomationResponseTeamsReplyAccessTeamsUsers))
	}
	return types.StringValue(string(*result.TeamsReplyAccess))
}

// normalizeRunAsState returns the refreshed state value for run_as. A null
// prior state is preserved while the server holds the organization default
// (null and "organization" are equivalent), so config-omitted run_as does
// not plan a perpetual no-op reset; any other server value surfaces as
// drift and is repaired on the next apply (Update always restates run_as).
func normalizeRunAsState(server string, prior types.String) types.String {
	if server == "organization" && (prior.IsNull() || prior.ValueString() == "organization") {
		return prior
	}
	return types.StringValue(server)
}

// automationWebhook extracts the webhook inbox URL and secret from the
// response triggers (the API allows at most one webhook:incoming trigger).
// The secret is only present when newly minted, so priorSecret is carried
// forward when the webhook persists without re-minting.
func automationWebhook(result *api.AutomationResponse, priorSecret types.String) (types.String, types.String) {
	for _, trigger := range result.Triggers {
		if !trigger.Webhook.IsSpecified() || trigger.Webhook.IsNull() {
			continue
		}
		webhook := trigger.Webhook.MustGet()
		secret := stringFromNullable(webhook.Secret)
		if secret.IsNull() {
			secret = priorSecret
		}
		return types.StringValue(webhook.URL), secret
	}
	return types.StringNull(), types.StringNull()
}
