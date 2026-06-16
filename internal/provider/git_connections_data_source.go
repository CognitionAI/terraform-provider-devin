package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &gitConnectionsDataSource{}

type gitConnectionsDataSource struct {
	client *Client
}

type gitConnectionModel struct {
	GitConnectionID types.String `tfsdk:"git_connection_id"`
	GitProviderType types.String `tfsdk:"git_provider_type"`
	Name            types.String `tfsdk:"name"`
	Host            types.String `tfsdk:"host"`
}

type gitConnectionsDataSourceModel struct {
	Connections []gitConnectionModel `tfsdk:"connections"`
}

func NewGitConnectionsDataSource() datasource.DataSource {
	return &gitConnectionsDataSource{}
}

func (d *gitConnectionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_git_connections"
}

func (d *gitConnectionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the git connections available to the enterprise, e.g. to look up the git_connection_id used by devin_git_permission.",
		Attributes: map[string]schema.Attribute{
			"connections": schema.ListNestedAttribute{
				Description: "Git connections for the enterprise.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"git_connection_id": schema.StringAttribute{
							Description: "Git connection ID.",
							Computed:    true,
						},
						"git_provider_type": schema.StringAttribute{
							Description: "Provider type (e.g. github_app, gitlab_oauth).",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Display name of the connection.",
							Computed:    true,
						},
						"host": schema.StringAttribute{
							Description: "Git host the connection points at (e.g. github.com).",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *gitConnectionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *gitConnectionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state gitConnectionsDataSourceModel
	state.Connections = []gitConnectionModel{}

	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}

		var page api.PaginatedResponseGitConnectionResponse
		if err := d.client.Get(ctx, enterpriseGitConnectionsPath+"?"+query.Encode(), &page); err != nil {
			resp.Diagnostics.AddError("Failed to list git connections", err.Error())
			return
		}

		for _, item := range page.Items {
			state.Connections = append(state.Connections, gitConnectionModel{
				GitConnectionID: types.StringValue(item.GitConnectionID),
				GitProviderType: types.StringValue(string(item.GitProviderType)),
				Name:            stringFromNullable(item.Name),
				Host:            types.StringValue(item.Host),
			})
		}

		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			break
		}
		cursor = page.EndCursor.MustGet()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
