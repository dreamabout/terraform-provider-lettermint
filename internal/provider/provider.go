// Package provider implements the lettermint provider with
// terraform-plugin-framework.
package provider

import (
	"context"
	"errors"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

var _ provider.Provider = &lettermintProvider{}

type lettermintProvider struct {
	version string
}

type lettermintProviderModel struct {
	Token   types.String `tfsdk:"token"`
	BaseURL types.String `tfsdk:"base_url"`
}

// New returns a constructor for the provider at the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &lettermintProvider{version: version}
	}
}

func (p *lettermintProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "lettermint"
	resp.Version = p.version
}

func (p *lettermintProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages domains and inbound routes in Lettermint through the Team API.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Description: "Team API token (lm_team_…). Defaults to the LETTERMINT_TOKEN environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"base_url": schema.StringAttribute{
				Description: "Team API base URL. Defaults to the LETTERMINT_BASE_URL environment variable, or " + client.DefaultBaseURL + ".",
				Optional:    true,
			},
		},
	}
}

func (p *lettermintProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg lettermintProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown value (from another resource not yet applied) is left to
	// the environment; resources then fail on use rather than at plan.
	token, baseURL, err := resolveConfig(cfg.Token.ValueString(), cfg.BaseURL.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Missing Lettermint token", err.Error())
		return
	}

	c := client.New(baseURL, token, "terraform-provider-lettermint/"+p.version)
	resp.DataSourceData = c
	resp.ResourceData = c
}

// resolveConfig picks the token and base URL from the provider block, then
// the environment, then the default.
func resolveConfig(attrToken, attrBaseURL string) (token, baseURL string, err error) {
	token = attrToken
	if token == "" {
		token = os.Getenv("LETTERMINT_TOKEN")
	}
	if token == "" {
		return "", "", errors.New("set the token attribute or the LETTERMINT_TOKEN environment variable to a Lettermint Team API token")
	}

	baseURL = attrBaseURL
	if baseURL == "" {
		baseURL = os.Getenv("LETTERMINT_BASE_URL")
	}
	if baseURL == "" {
		baseURL = client.DefaultBaseURL
	}
	return token, baseURL, nil
}

func (p *lettermintProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newDomainResource,
		newDomainVerificationResource,
		newRouteInboundResource,
	}
}

func (p *lettermintProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newDomainDataSource,
		newDomainsDataSource,
		newRouteDataSource,
	}
}
