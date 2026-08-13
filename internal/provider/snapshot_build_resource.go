package provider

import (
	"context"
	"time"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &snapshotBuildResource{}
var _ resource.ResourceWithImportState = &snapshotBuildResource{}
var _ resource.ResourceWithIdentity = &snapshotBuildResource{}

type snapshotBuildResource struct {
	client *Client
}

type snapshotBuildModel struct {
	BuildID           types.String `tfsdk:"build_id"`
	OrgID             types.String `tfsdk:"org_id"`
	Status            types.String `tfsdk:"status"`
	Trigger           types.String `tfsdk:"trigger"`
	Pinned            types.Bool   `tfsdk:"pinned"`
	StartedAt         types.Int64  `tfsdk:"started_at"`
	CompletedAt       types.Int64  `tfsdk:"completed_at"`
	CreatedAt         types.Int64  `tfsdk:"created_at"`
	UpdatedAt         types.Int64  `tfsdk:"updated_at"`
	TriggeredByUserID types.String `tfsdk:"triggered_by_user_id"`
	WaitUntilComplete types.Bool   `tfsdk:"wait_until_complete"`
}

func NewSnapshotBuildResource() resource.Resource {
	return &snapshotBuildResource{}
}

func (r *snapshotBuildResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_snapshot_build"
}

func (r *snapshotBuildResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Triggers and tracks a Devin snapshot build. Mutating blueprints does not auto-build; use this resource after blueprint changes.",
		Attributes: map[string]schema.Attribute{
			"build_id": schema.StringAttribute{
				Description: "Build ID assigned by Devin.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID to build snapshots for.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Description: "Build status: pending, running, succeeded, failed, or cancelled.",
				Computed:    true,
			},
			"trigger": schema.StringAttribute{
				Description: "How the build was triggered (manual or auto).",
				Computed:    true,
			},
			"pinned": schema.BoolAttribute{
				Description: "Whether this build is pinned for the organization.",
				Computed:    true,
			},
			"started_at": schema.Int64Attribute{
				Description: "Unix timestamp when the build started.",
				Computed:    true,
			},
			"completed_at": schema.Int64Attribute{
				Description: "Unix timestamp when the build completed.",
				Computed:    true,
			},
			"created_at": schema.Int64Attribute{
				Description: "Unix timestamp when the build was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.Int64Attribute{
				Description: "Unix timestamp when the build was last updated.",
				Computed:    true,
			},
			"triggered_by_user_id": schema.StringAttribute{
				Description: "User ID that triggered the build.",
				Computed:    true,
			},
			"wait_until_complete": schema.BoolAttribute{
				Description: "If true, block until the build reaches a terminal status (succeeded, failed, or cancelled).",
				Optional:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *snapshotBuildResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = snapshotBuildIdentitySchema()
}

func (r *snapshotBuildResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *snapshotBuildResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan snapshotBuildModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.SnapshotBuildResponse
	if err := r.client.Post(ctx, orgSnapshotBuildsPath(plan.OrgID.ValueString()), api.SnapshotBuildTriggerRequest{}, &result); err != nil {
		resp.Diagnostics.AddError("Failed to trigger snapshot build", err.Error())
		return
	}

	if !plan.WaitUntilComplete.IsNull() && plan.WaitUntilComplete.ValueBool() {
		var err error
		result, err = r.waitForTerminalBuild(ctx, plan.OrgID.ValueString(), result.BuildID)
		if err != nil {
			resp.Diagnostics.AddError("Snapshot build did not complete", err.Error())
			return
		}
	}

	mapSnapshotBuildResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, snapshotBuildIdentityModel{OrgID: plan.OrgID, BuildID: plan.BuildID}, &resp.Diagnostics)
}

func (r *snapshotBuildResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state snapshotBuildModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.SnapshotBuildResponse
	err := r.client.Get(ctx, orgSnapshotBuildPath(state.OrgID.ValueString(), state.BuildID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, snapshotBuildIdentityModel{OrgID: state.OrgID, BuildID: state.BuildID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read snapshot build", err.Error())
		return
	}

	mapSnapshotBuildResponseToModel(&result, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, snapshotBuildIdentityModel{OrgID: state.OrgID, BuildID: state.BuildID}, &resp.Diagnostics)
}

func (r *snapshotBuildResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// Builds are immutable once triggered.
}

func (r *snapshotBuildResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state snapshotBuildModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	status := state.Status.ValueString()
	if status == "pending" || status == "running" {
		err := r.client.Post(ctx, orgSnapshotBuildCancelPath(state.OrgID.ValueString(), state.BuildID.ValueString()), struct{}{}, nil)
		if err != nil && !IsNotFound(err) {
			resp.Diagnostics.AddError("Failed to cancel snapshot build", err.Error())
		}
	}
}

func (r *snapshotBuildResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "org_id", "build_id")
}

func (r *snapshotBuildResource) waitForTerminalBuild(ctx context.Context, orgID, buildID string) (api.SnapshotBuildResponse, error) {
	deadline := time.Now().Add(30 * time.Minute)
	for {
		var result api.SnapshotBuildResponse
		if err := r.client.Get(ctx, orgSnapshotBuildPath(orgID, buildID), &result); err != nil {
			return api.SnapshotBuildResponse{}, err
		}
		switch result.Status {
		case "succeeded", "failed", "cancelled":
			return result, nil
		}
		if time.Now().After(deadline) {
			return result, &APIError{StatusCode: 408, Detail: "timed out waiting for snapshot build"}
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

func mapSnapshotBuildResponseToModel(resp *api.SnapshotBuildResponse, model *snapshotBuildModel) {
	model.BuildID = types.StringValue(resp.BuildID)
	model.Status = types.StringValue(resp.Status)
	model.Trigger = types.StringValue(resp.Trigger)
	model.Pinned = types.BoolValue(resp.Pinned)
	model.CreatedAt = types.Int64Value(resp.CreatedAt)
	model.UpdatedAt = types.Int64Value(resp.UpdatedAt)
	model.StartedAt = int64FromOptionalPtr(resp.StartedAt)
	model.CompletedAt = int64FromOptionalPtr(resp.CompletedAt)
	model.TriggeredByUserID = stringFromOptionalPtr(resp.TriggeredByUserID)
}

func int64FromOptionalPtr(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}
