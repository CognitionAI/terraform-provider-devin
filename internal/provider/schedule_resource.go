package provider

import (
	"context"
	"time"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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

var _ resource.Resource = &scheduleResource{}
var _ resource.ResourceWithImportState = &scheduleResource{}
var _ resource.ResourceWithValidateConfig = &scheduleResource{}
var _ resource.ResourceWithIdentity = &scheduleResource{}

type scheduleResource struct {
	client *Client
}

type scheduleModel struct {
	ScheduleID     types.String      `tfsdk:"schedule_id"`
	OrgID          types.String      `tfsdk:"org_id"`
	Name           types.String      `tfsdk:"name"`
	Prompt         types.String      `tfsdk:"prompt"`
	PlaybookID     types.String      `tfsdk:"playbook_id"`
	Frequency      types.String      `tfsdk:"frequency"`
	IntervalCount  types.Int64       `tfsdk:"interval_count"`
	ScheduleType   types.String      `tfsdk:"schedule_type"`
	ScheduledAt    timetypes.RFC3339 `tfsdk:"scheduled_at"`
	Enabled        types.Bool        `tfsdk:"enabled"`
	NotifyOn       types.String      `tfsdk:"notify_on"`
	Agent          types.String      `tfsdk:"agent"`
	BypassApproval types.Bool        `tfsdk:"bypass_approval"`
	Tags           types.List        `tfsdk:"tags"`
	Platform       types.String      `tfsdk:"platform"`
	SlackChannelID types.String      `tfsdk:"slack_channel_id"`
	SlackTeamID    types.String      `tfsdk:"slack_team_id"`
	TargetDevinID  types.String      `tfsdk:"target_devin_id"`
}

func NewScheduleResource() resource.Resource {
	return &scheduleResource{}
}

func (r *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func (r *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a scheduled Devin session (recurring cron schedule or one-time run) within an organization.",
		Attributes: map[string]schema.Attribute{
			"schedule_id": schema.StringAttribute{
				Description: "Schedule ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns this schedule.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the schedule.",
				Required:    true,
			},
			"prompt": schema.StringAttribute{
				Description: "Prompt used to start each scheduled session.",
				Required:    true,
			},
			"playbook_id": schema.StringAttribute{
				Description: "Playbook to run for each scheduled session.",
				Optional:    true,
			},
			"frequency": schema.StringAttribute{
				Description: "Cron expression for recurring schedules (e.g. '0 9 * * 1'). Required when schedule_type is 'recurring'.",
				Optional:    true,
			},
			"interval_count": schema.Int64Attribute{
				Description: "Run every N occurrences of the cron expression. Defaults to 1.",
				Optional:    true,
				Computed:    true,
			},
			"schedule_type": schema.StringAttribute{
				Description: "Either 'recurring' (default) or 'one_time'.",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("recurring", "one_time"),
				},
			},
			"scheduled_at": schema.StringAttribute{
				Description: "RFC3339 timestamp for one-time schedules. Must be in the future and include a timezone.",
				Optional:    true,
				CustomType:  timetypes.RFC3339Type{},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the schedule is active. Defaults to true.",
				Optional:    true,
				Computed:    true,
			},
			"notify_on": schema.StringAttribute{
				Description: "When to notify about scheduled runs: 'always', 'failure' (default), or 'never'.",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("always", "failure", "never"),
				},
			},
			"agent": schema.StringAttribute{
				Description: "Agent that runs the scheduled sessions: 'devin' (default) or 'data_analyst'.",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("devin", "data_analyst"),
				},
			},
			"bypass_approval": schema.BoolAttribute{
				Description: "Whether scheduled sessions skip the plan approval step. Defaults to false.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"tags": schema.ListAttribute{
				Description: "Session tags applied to sessions created by this schedule.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"platform": schema.StringAttribute{
				Description: "VM platform for sessions spawned by this schedule (e.g. 'windows'). Must match a platform label configured for the organization.",
				Optional:    true,
			},
			"slack_channel_id": schema.StringAttribute{
				Description: "Slack channel to post scheduled session updates to.",
				Optional:    true,
			},
			"slack_team_id": schema.StringAttribute{
				Description: "Slack team the channel belongs to.",
				Optional:    true,
			},
			"target_devin_id": schema.StringAttribute{
				Description: "Existing session to send the prompt to instead of creating a new one. Only allowed for one_time schedules.",
				Optional:    true,
			},
		},
	}
}

// ValidateConfig mirrors the API's cross-field rules so invalid combinations
// fail at plan time instead of at apply.
func (r *scheduleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config scheduleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.ScheduleType.IsUnknown() {
		return
	}

	if config.ScheduleType.ValueString() == "one_time" {
		if config.ScheduledAt.IsNull() {
			resp.Diagnostics.AddAttributeError(
				path.Root("scheduled_at"),
				"Missing required attribute",
				`scheduled_at is required when schedule_type is "one_time".`,
			)
		}
		if !config.Frequency.IsNull() && !config.Frequency.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root("frequency"),
				"Invalid attribute combination",
				`frequency must not be set when schedule_type is "one_time".`,
			)
		}
		return
	}

	// schedule_type defaults to "recurring" when omitted.
	if config.Frequency.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("frequency"),
			"Missing required attribute",
			`frequency is required for recurring schedules (the default schedule_type).`,
		)
	}
	if !config.TargetDevinID.IsNull() && !config.TargetDevinID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("target_devin_id"),
			"Invalid attribute combination",
			`target_devin_id is only allowed when schedule_type is "one_time".`,
		)
	}
}

func (r *scheduleResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = scheduleIdentitySchema()
}

func (r *scheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.ScheduleCreateRequest{
		Name:           plan.Name.ValueString(),
		Prompt:         plan.Prompt.ValueString(),
		PlaybookID:     optionalStringFrom(plan.PlaybookID),
		Frequency:      optionalStringFrom(plan.Frequency),
		IntervalCount:  intPtrFrom(plan.IntervalCount),
		BypassApproval: boolPtrFrom(plan.BypassApproval),
		Platform:       optionalStringFrom(plan.Platform),
		SlackChannelID: optionalStringFrom(plan.SlackChannelID),
		SlackTeamID:    optionalStringFrom(plan.SlackTeamID),
		TargetDevinID:  optionalStringFrom(plan.TargetDevinID),
	}
	if !plan.ScheduleType.IsNull() && !plan.ScheduleType.IsUnknown() {
		scheduleType := api.ScheduleCreateRequestScheduleType(plan.ScheduleType.ValueString())
		body.ScheduleType = &scheduleType
	}
	if !plan.NotifyOn.IsNull() && !plan.NotifyOn.IsUnknown() {
		notifyOn := api.ScheduleCreateRequestNotifyOn(plan.NotifyOn.ValueString())
		body.NotifyOn = &notifyOn
	}
	if !plan.Agent.IsNull() && !plan.Agent.IsUnknown() {
		agent := api.ScheduleCreateRequestAgent(plan.Agent.ValueString())
		body.Agent = &agent
	}
	if !plan.ScheduledAt.IsNull() {
		scheduledAt, diags := plan.ScheduledAt.ValueRFC3339Time()
		resp.Diagnostics.Append(diags...)
		body.ScheduledAt = nullable.NewNullableWithValue(scheduledAt)
	}
	if !plan.Tags.IsNull() {
		body.Tags = nullable.NewNullableWithValue(listToStrings(ctx, plan.Tags, &resp.Diagnostics))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Capture the desired enabled state before the API response overwrites it
	// (mapScheduleResponseToModel sets plan.Enabled from the response, which
	// is always true since the create endpoint has no enabled field).
	wantDisabled := !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() && !plan.Enabled.ValueBool()

	var result api.ScheduleResponse
	if err := r.client.Post(ctx, orgSchedulesPath(plan.OrgID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to create schedule", err.Error())
		return
	}

	// Save state and identity immediately so the resource is tracked even if
	// the optional disable PATCH below fails.
	resp.Diagnostics.Append(mapScheduleResponseToModel(ctx, &result, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, scheduleIdentityModel{OrgID: plan.OrgID, ScheduleID: plan.ScheduleID}, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// The create endpoint cannot disable a schedule; apply an explicit
	// enabled = false from the plan with a follow-up update.
	if wantDisabled {
		patchPath := orgSchedulePath(plan.OrgID.ValueString(), result.ScheduledSessionID)
		disable := api.ScheduleUpdateRequest{Enabled: nullable.NewNullableWithValue(false)}
		if err := r.client.Patch(ctx, patchPath, disable, &result); err != nil {
			resp.Diagnostics.AddError("Failed to disable schedule after creation", err.Error())
			return
		}
		resp.Diagnostics.Append(mapScheduleResponseToModel(ctx, &result, &plan)...)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.ScheduleResponse
	err := r.client.Get(ctx, orgSchedulePath(state.OrgID.ValueString(), state.ScheduleID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, scheduleIdentityModel{OrgID: state.OrgID, ScheduleID: state.ScheduleID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(mapScheduleResponseToModel(ctx, &result, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, scheduleIdentityModel{OrgID: state.OrgID, ScheduleID: state.ScheduleID}, &resp.Diagnostics)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The PATCH endpoint uses field-set semantics: fields that support
	// clearing are always included so removing them from config clears them
	// on the API side; explicit null clears, omission leaves unchanged.
	body := api.ScheduleUpdateRequest{
		Name:           nullable.NewNullableWithValue(plan.Name.ValueString()),
		Prompt:         nullable.NewNullableWithValue(plan.Prompt.ValueString()),
		PlaybookID:     nullableStringFrom(plan.PlaybookID),
		Frequency:      optionalStringFrom(plan.Frequency),
		IntervalCount:  optionalIntFrom(plan.IntervalCount),
		Platform:       nullableStringFrom(plan.Platform),
		SlackChannelID: nullableStringFrom(plan.SlackChannelID),
		SlackTeamID:    nullableStringFrom(plan.SlackTeamID),
		TargetDevinID:  nullableStringFrom(plan.TargetDevinID),
	}
	if !plan.ScheduleType.IsNull() && !plan.ScheduleType.IsUnknown() {
		body.ScheduleType = nullable.NewNullableWithValue(api.ScheduleUpdateRequestScheduleType(plan.ScheduleType.ValueString()))
	}
	if !plan.NotifyOn.IsNull() && !plan.NotifyOn.IsUnknown() {
		body.NotifyOn = nullable.NewNullableWithValue(api.ScheduleUpdateRequestNotifyOn(plan.NotifyOn.ValueString()))
	}
	if !plan.Agent.IsNull() && !plan.Agent.IsUnknown() {
		body.Agent = nullable.NewNullableWithValue(api.ScheduleUpdateRequestAgent(plan.Agent.ValueString()))
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		body.Enabled = nullable.NewNullableWithValue(plan.Enabled.ValueBool())
	}
	if !plan.BypassApproval.IsNull() && !plan.BypassApproval.IsUnknown() {
		body.BypassApproval = nullable.NewNullableWithValue(plan.BypassApproval.ValueBool())
	}
	// Only send scheduled_at when it actually changed; resending an
	// expired timestamp on an unrelated update would be rejected by the API.
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.ScheduledAt.Equal(state.ScheduledAt) {
		if plan.ScheduledAt.IsNull() {
			body.ScheduledAt = nullable.NewNullNullable[time.Time]()
		} else {
			scheduledAt, diags := plan.ScheduledAt.ValueRFC3339Time()
			resp.Diagnostics.Append(diags...)
			body.ScheduledAt = nullable.NewNullableWithValue(scheduledAt)
		}
	}
	if plan.Tags.IsNull() {
		body.Tags = nullable.NewNullNullable[[]string]()
	} else {
		body.Tags = nullable.NewNullableWithValue(listToStrings(ctx, plan.Tags, &resp.Diagnostics))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.ScheduleResponse
	if err := r.client.Patch(ctx, orgSchedulePath(plan.OrgID.ValueString(), plan.ScheduleID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(mapScheduleResponseToModel(ctx, &result, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, scheduleIdentityModel{OrgID: plan.OrgID, ScheduleID: plan.ScheduleID}, &resp.Diagnostics)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, orgSchedulePath(state.OrgID.ValueString(), state.ScheduleID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete schedule", err.Error())
	}
}

func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "schedule_id")
}

func mapScheduleResponseToModel(ctx context.Context, resp *api.ScheduleResponse, model *scheduleModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ScheduleID = types.StringValue(resp.ScheduledSessionID)
	model.OrgID = types.StringValue(resp.OrgID)
	model.Name = types.StringValue(resp.Name)
	model.Prompt = types.StringValue(resp.Prompt)
	model.Enabled = types.BoolValue(resp.Enabled)
	model.NotifyOn = types.StringValue(string(resp.NotifyOn))
	model.Agent = types.StringValue(string(resp.Agent))

	scheduleType := string(api.ScheduleResponseScheduleTypeRecurring)
	if resp.ScheduleType != nil {
		scheduleType = string(*resp.ScheduleType)
	}
	model.ScheduleType = types.StringValue(scheduleType)

	intervalCount := int64(1)
	if resp.IntervalCount != nil {
		intervalCount = int64(*resp.IntervalCount)
	}
	model.IntervalCount = types.Int64Value(intervalCount)

	bypassApproval := false
	if resp.BypassApproval != nil {
		bypassApproval = *resp.BypassApproval
	}
	model.BypassApproval = types.BoolValue(bypassApproval)

	if resp.Playbook.IsSpecified() && !resp.Playbook.IsNull() {
		model.PlaybookID = types.StringValue(resp.Playbook.MustGet().PlaybookID)
	} else {
		model.PlaybookID = types.StringNull()
	}
	// The API keeps the last cron expression around when a schedule is
	// switched to one_time; it is ignored in that mode, so only surface it
	// for recurring schedules.
	if scheduleType == string(api.ScheduleResponseScheduleTypeRecurring) {
		model.Frequency = stringFromNullable(resp.Frequency)
	} else {
		model.Frequency = types.StringNull()
	}
	model.Platform = stringFromNullable(resp.Platform)
	model.SlackChannelID = stringFromNullable(resp.SlackChannelID)
	model.SlackTeamID = stringFromNullable(resp.SlackTeamID)
	model.TargetDevinID = stringFromNullable(resp.TargetDevinID)
	model.ScheduledAt = rfc3339FromNullable(resp.ScheduledAt)

	if resp.Tags.IsSpecified() && !resp.Tags.IsNull() && len(resp.Tags.MustGet()) > 0 {
		tags, d := types.ListValueFrom(ctx, types.StringType, resp.Tags.MustGet())
		diags.Append(d...)
		model.Tags = tags
	} else {
		model.Tags = types.ListNull(types.StringType)
	}

	return diags
}

func listToStrings(ctx context.Context, list types.List, diags *diag.Diagnostics) []string {
	var values []string
	diags.Append(list.ElementsAs(ctx, &values, false)...)
	return values
}
