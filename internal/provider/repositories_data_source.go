package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &repositoriesDataSource{}

type repositoriesDataSource struct {
	client *Client
}

type repositoryModel struct {
	RepositoryPath       types.String `tfsdk:"repository_path"`
	RepoPath             types.String `tfsdk:"repo_path"`
	RepoName             types.String `tfsdk:"repo_name"`
	RepoDescription      types.String `tfsdk:"repo_description"`
	RepoLanguage         types.String `tfsdk:"repo_language"`
	GitConnectionID      types.String `tfsdk:"git_connection_id"`
	GitConnectionHost    types.String `tfsdk:"git_connection_host"`
	ProviderRepositoryID types.String `tfsdk:"provider_repository_id"`
	LastUpdatedAt        types.Int64  `tfsdk:"last_updated_at"`
	IndexingEnabled      types.Bool   `tfsdk:"indexing_enabled"`
}

type repositoriesDataSourceModel struct {
	OrgID        types.String      `tfsdk:"org_id"`
	FilterName   types.String      `tfsdk:"filter_name"`
	Repositories []repositoryModel `tfsdk:"repositories"`
}

func NewRepositoriesDataSource() datasource.DataSource {
	return &repositoriesDataSource{}
}

func (d *repositoriesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repositories"
}

func (d *repositoriesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Description: description, Computed: true}
	}
	resp.Schema = schema.Schema{
		Description: "Lists the repositories connected to an organization, e.g. to look up the repository_path used by " +
			"devin_repository_indexing and devin_blueprint.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID.",
				Required:    true,
			},
			"filter_name": schema.StringAttribute{
				Description: "Only return repositories whose path contains this string (case-insensitive).",
				Optional:    true,
			},
			"repositories": schema.ListNestedAttribute{
				Description: "Connected repositories.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"repository_path": computedString("Devin's identifier for the repository, the value devin_repository_indexing " +
							"and devin_blueprint take: the path for github.com (e.g. acme/web), prefixed with the host for other " +
							"git hosts (e.g. gitlab.example.com/acme/web), or a perforce:// URI for Perforce depots."),
						"repo_path":              computedString("Repository path at the git provider, without the host (e.g. acme/web)."),
						"repo_name":              computedString("Repository name."),
						"repo_description":       computedString("Repository description from the git provider."),
						"repo_language":          computedString("Primary language reported by the git provider."),
						"git_connection_id":      computedString("Git connection the repository comes from."),
						"git_connection_host":    computedString("Git host of that connection (e.g. github.com)."),
						"provider_repository_id": computedString("Repository ID at the git provider."),
						"last_updated_at": schema.Int64Attribute{
							Description: "Last update time reported by the git provider (Unix seconds).",
							Computed:    true,
						},
						"indexing_enabled": schema.BoolAttribute{
							Description: "Whether indexing is enabled for the repository.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *repositoriesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *repositoriesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state repositoriesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Repositories = []repositoryModel{}

	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if !state.FilterName.IsNull() && state.FilterName.ValueString() != "" {
			query.Set("filter_name", state.FilterName.ValueString())
		}
		if cursor != "" {
			query.Set("after", cursor)
		}

		var page api.PaginatedResponseRepositoryResponse
		if err := d.client.Get(ctx, orgRepositoriesPath(state.OrgID.ValueString())+"?"+query.Encode(), &page); err != nil {
			resp.Diagnostics.AddError("Failed to list repositories", err.Error())
			return
		}

		for _, item := range page.Items {
			indexingEnabled := false
			if item.IndexingStatus.IsSpecified() && !item.IndexingStatus.IsNull() {
				indexingEnabled = item.IndexingStatus.MustGet().IndexingEnabled
			}
			state.Repositories = append(state.Repositories, repositoryModel{
				RepositoryPath:       types.StringValue(item.RepoFullName),
				RepoPath:             types.StringValue(item.RepoPath),
				RepoName:             types.StringValue(item.RepoName),
				RepoDescription:      stringFromNullable(item.RepoDescription),
				RepoLanguage:         stringFromNullable(item.RepoLanguage),
				GitConnectionID:      types.StringValue(item.GitConnectionID),
				GitConnectionHost:    types.StringValue(item.GitConnectionHost),
				ProviderRepositoryID: types.StringValue(item.ProviderRepositoryID),
				LastUpdatedAt:        int64FromNullable(item.LastUpdatedAt),
				IndexingEnabled:      types.BoolValue(indexingEnabled),
			})
		}

		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			break
		}
		cursor = page.EndCursor.MustGet()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
