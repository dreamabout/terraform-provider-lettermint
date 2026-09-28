package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

var _ datasource.DataSourceWithConfigure = &domainsDataSource{}

func newDomainsDataSource() datasource.DataSource { return &domainsDataSource{} }

type domainsDataSource struct {
	client *client.Client
}

type domainsDataModel struct {
	Domains types.List `tfsdk:"domains"`
}

var domainSummaryAttrTypes = map[string]attr.Type{
	"id":        types.StringType,
	"domain":    types.StringType,
	"status":    types.StringType,
	"dkim_mode": types.StringType,
}

func (d *domainsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domains"
}

func (d *domainsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists every domain in the team, subdomains included, sorted by name. " +
			"Useful in a check block to find domains that the configuration does not manage.",
		Attributes: map[string]schema.Attribute{
			"domains": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The team's domains.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":        schema.StringAttribute{Computed: true, Description: "Domain id."},
						"domain":    schema.StringAttribute{Computed: true, Description: "Domain name."},
						"status":    schema.StringAttribute{Computed: true, Description: "verified, partially_verified, pending_verification or failed_verification."},
						"dkim_mode": schema.StringAttribute{Computed: true, Description: "legacy_txt or managed_cname."},
					},
				},
			},
		},
	}
}

func (d *domainsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = configureDataSource(req)
}

func (d *domainsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	domains, err := d.client.ListDomains(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Listing Lettermint domains", err.Error())
		return
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].Domain < domains[j].Domain })

	objType := types.ObjectType{AttrTypes: domainSummaryAttrTypes}
	elems := make([]attr.Value, 0, len(domains))
	for _, dom := range domains {
		obj, diags := types.ObjectValue(domainSummaryAttrTypes, map[string]attr.Value{
			"id":        types.StringValue(dom.ID),
			"domain":    types.StringValue(dom.Domain),
			"status":    types.StringValue(dom.Status),
			"dkim_mode": types.StringValue(dom.DkimMode),
		})
		resp.Diagnostics.Append(diags...)
		elems = append(elems, obj)
	}
	list, diags := types.ListValue(objType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, domainsDataModel{Domains: list})...)
}
