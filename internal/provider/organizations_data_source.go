package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &organizationsDataSource{}

type organizationsDataSource struct {
	client *Client
}

type organizationSummaryModel struct {
	OrgID              types.String `tfsdk:"org_id"`
	Name               types.String `tfsdk:"name"`
	MaxSessionACULimit types.Int64  `tfsdk:"max_session_acu_limit"`
	MaxCycleACULimit   types.Int64  `tfsdk:"max_cycle_acu_limit"`
}

type organizationsDataSourceModel struct {
	Organizations []organizationSummaryModel `tfsdk:"organizations"`
}

func NewOrganizationsDataSource() datasource.DataSource {
	return &organizationsDataSource{}
}

func (d *organizationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organizations"
}

func (d *organizationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the organizations in the enterprise, e.g. to look up an org_id or adopt an existing org without importing it.",
		Attributes: map[string]schema.Attribute{
			"organizations": schema.ListNestedAttribute{
				Description: "Organizations in the enterprise.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"org_id": schema.StringAttribute{
							Description: "Organization ID.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Display name of the organization.",
							Computed:    true,
						},
						"max_session_acu_limit": schema.Int64Attribute{
							Description: "Maximum ACU limit per session, if set.",
							Computed:    true,
						},
						"max_cycle_acu_limit": schema.Int64Attribute{
							Description: "Maximum ACU limit per cycle, if set.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *organizationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *organizationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state organizationsDataSourceModel
	state.Organizations = []organizationSummaryModel{}

	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}

		var page api.PaginatedResponseOrganizationResponse
		if err := d.client.Get(ctx, enterpriseOrganizationsPath+"?"+query.Encode(), &page); err != nil {
			resp.Diagnostics.AddError("Failed to list organizations", err.Error())
			return
		}

		for _, item := range page.Items {
			state.Organizations = append(state.Organizations, organizationSummaryModel{
				OrgID:              types.StringValue(item.OrgID),
				Name:               types.StringValue(item.Name),
				MaxSessionACULimit: int64FromNullable(item.MaxSessionAcuLimit),
				MaxCycleACULimit:   int64FromNullable(item.MaxCycleAcuLimit),
			})
		}

		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			break
		}
		cursor = page.EndCursor.MustGet()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
