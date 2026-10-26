package provider

import (
	"context"
	"encoding/json"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &oncallSchemasDataSource{}

type oncallSchemasDataSource struct {
	client *Client
}

type oncallSchemasDataSourceModel struct {
	OrgID                    types.String         `tfsdk:"org_id"`
	Sources                  jsontypes.Normalized `tfsdk:"sources"`
	DefaultRunbook           types.String         `tfsdk:"default_runbook"`
	AvailableDevinModes      types.List           `tfsdk:"available_devin_modes"`
	DefaultDevinMode         types.String         `tfsdk:"default_devin_mode"`
	DefaultNetPolicy         jsontypes.Normalized `tfsdk:"default_net_policy"`
	SecurityProfile          jsontypes.Normalized `tfsdk:"security_profile"`
	PagerdutyConnectionID    types.String         `tfsdk:"pagerduty_connection_id"`
	SlackConnected           types.Bool           `tfsdk:"slack_connected"`
	TeamsConnected           types.Bool           `tfsdk:"teams_connected"`
	LinearConnected          types.Bool           `tfsdk:"linear_connected"`
	JiraConnected            types.Bool           `tfsdk:"jira_connected"`
	PylonConnected           types.Bool           `tfsdk:"pylon_connected"`
	WebhookRespondersEnabled types.Bool           `tfsdk:"webhook_responders_enabled"`
}

func NewOncallSchemasDataSource() datasource.DataSource {
	return &oncallSchemasDataSource{}
}

func (d *oncallSchemasDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_oncall_schemas"
}

func (d *oncallSchemasDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Description: description, Computed: true}
	}
	computedJSON := func(description string) schema.StringAttribute {
		return schema.StringAttribute{CustomType: jsontypes.NormalizedType{}, Description: description, Computed: true}
	}
	computedBool := func(description string) schema.BoolAttribute {
		return schema.BoolAttribute{Description: description, Computed: true}
	}
	resp.Schema = schema.Schema{
		Description: "Describes what devin_oncall_responder accepts for an organization: the trigger event types " +
			"and condition fields per alert source, the connected sources, the PagerDuty connection to use, and " +
			"the defaults applied when runbook or session_settings are omitted.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID.",
				Required:    true,
			},
			"sources": computedJSON("JSON-encoded map of source (slack, teams, pagerduty, webhook) -> event type -> " +
				"schema ({event_type, name, category, fields}); use jsondecode(...). Only available sources are listed."),
			"default_runbook": computedString("Runbook applied when a responder's runbook is blank."),
			"available_devin_modes": schema.ListAttribute{
				ElementType: types.StringType,
				Description: "Every session_settings.devin_mode the organization may set on responders.",
				Computed:    true,
			},
			"default_devin_mode": computedString("Mode applied when session_settings.devin_mode is omitted; null " +
				"when sessions inherit the organization default."),
			"default_net_policy": computedJSON("JSON-encoded network policy applied when session_settings.net_policy " +
				"is omitted; null when the governing security profile sets one."),
			"security_profile": computedJSON("JSON-encoded security profile governing responder sessions; null when none applies."),
			"pagerduty_connection_id": computedString("PagerDuty connection to pass as devin_oncall_responder." +
				"pagerduty_connection_id; null when PagerDuty is not connected or not enabled."),
			"slack_connected":            computedBool("Whether the organization has a Slack connection."),
			"teams_connected":            computedBool("Whether Microsoft Teams responders are available."),
			"linear_connected":           computedBool("Whether Linear responders are available: the organization has a Linear connection and the feature is enabled."),
			"jira_connected":             computedBool("Whether Jira responders are available: the organization has a Jira connection and the feature is enabled."),
			"pylon_connected":            computedBool("Whether Pylon responders are available: the organization has a Pylon connection with automation triggers turned on and the feature is enabled."),
			"webhook_responders_enabled": computedBool("Whether webhook responders are available."),
		},
	}
}

func (d *oncallSchemasDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *oncallSchemasDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state oncallSchemasDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.OncallSchemasResponse
	if err := d.client.Get(ctx, orgOncallPath(state.OrgID.ValueString())+"/schemas", &result); err != nil {
		resp.Diagnostics.AddError("Failed to read Oncall schemas", err.Error())
		return
	}

	state.Sources = jsonValue(result.Sources)
	state.DefaultRunbook = types.StringValue(result.DefaultRunbook)
	modes := make([]string, 0, len(result.AvailableDevinModes))
	for _, mode := range result.AvailableDevinModes {
		modes = append(modes, string(mode))
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, modes)
	resp.Diagnostics.Append(diags...)
	state.AvailableDevinModes = list
	state.DefaultDevinMode = types.StringNull()
	if result.DefaultDevinMode.IsSpecified() && !result.DefaultDevinMode.IsNull() {
		state.DefaultDevinMode = types.StringValue(string(result.DefaultDevinMode.MustGet()))
	}
	state.DefaultNetPolicy = jsontypes.NewNormalizedNull()
	if result.DefaultNetPolicy.IsSpecified() && !result.DefaultNetPolicy.IsNull() {
		state.DefaultNetPolicy = jsonValue(result.DefaultNetPolicy.MustGet())
	}
	state.SecurityProfile = jsontypes.NewNormalizedNull()
	if result.SecurityProfile.IsSpecified() && !result.SecurityProfile.IsNull() {
		state.SecurityProfile = jsonValue(result.SecurityProfile.MustGet())
	}
	state.PagerdutyConnectionID = stringFromNullable(result.PagerdutyConnectionID)
	state.SlackConnected = types.BoolValue(result.SlackConnected)
	state.TeamsConnected = types.BoolValue(result.TeamsConnected != nil && *result.TeamsConnected)
	state.LinearConnected = types.BoolValue(result.LinearConnected != nil && *result.LinearConnected)
	state.JiraConnected = types.BoolValue(result.JiraConnected != nil && *result.JiraConnected)
	state.PylonConnected = types.BoolValue(result.PylonConnected != nil && *result.PylonConnected)
	state.WebhookRespondersEnabled = types.BoolValue(result.WebhookRespondersEnabled != nil && *result.WebhookRespondersEnabled)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func jsonValue(value any) jsontypes.Normalized {
	raw, err := json.Marshal(value)
	if err != nil {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(raw))
}
