package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &idpGroupsDataSource{}

type idpGroupsDataSource struct {
	client *Client
}

type idpGroupSummaryModel struct {
	IdpGroupName types.String `tfsdk:"idp_group_name"`
}

type idpGroupsDataSourceModel struct {
	IdpGroups []idpGroupSummaryModel `tfsdk:"idp_groups"`
}

func NewIdpGroupsDataSource() datasource.DataSource {
	return &idpGroupsDataSource{}
}

func (d *idpGroupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_idp_groups"
}

func (d *idpGroupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the IdP groups registered with the enterprise, e.g. to adopt existing groups when " +
			"assigning roles with devin_enterprise_idp_group_role or devin_org_idp_group_role.",
		Attributes: map[string]schema.Attribute{
			"idp_groups": schema.ListNestedAttribute{
				Description: "IdP groups registered with the enterprise.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"idp_group_name": schema.StringAttribute{
							Description: "Name of the IdP group.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *idpGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *idpGroupsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state idpGroupsDataSourceModel
	state.IdpGroups = []idpGroupSummaryModel{}

	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}

		var page api.PaginatedResponseIdpGroupResponse
		if err := d.client.Get(ctx, enterpriseIdpGroupsPath+"?"+query.Encode(), &page); err != nil {
			resp.Diagnostics.AddError("Failed to list IdP groups", err.Error())
			return
		}

		for _, item := range page.Items {
			state.IdpGroups = append(state.IdpGroups, idpGroupSummaryModel{
				IdpGroupName: types.StringValue(item.IdpGroupName),
			})
		}

		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			break
		}
		cursor = page.EndCursor.MustGet()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
