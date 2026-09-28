package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

var (
	_ datasource.DataSourceWithConfigure        = &domainDataSource{}
	_ datasource.DataSourceWithConfigValidators = &domainDataSource{}
	_ datasource.DataSourceWithConfigure        = &routeDataSource{}
)

func newDomainDataSource() datasource.DataSource { return &domainDataSource{} }
func newRouteDataSource() datasource.DataSource  { return &routeDataSource{} }

func configureDataSource(req datasource.ConfigureRequest) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	return req.ProviderData.(*client.Client)
}

type domainDataSource struct {
	client *client.Client
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up a Lettermint domain by id or by name.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Optional: true, Computed: true, Description: "Domain id. Set this or domain."},
			"domain":         schema.StringAttribute{Optional: true, Computed: true, Description: "Domain name. Set this or id."},
			"project_ids":    schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "Projects the domain is limited to."},
			"status":         schema.StringAttribute{Computed: true, Description: "verified, partially_verified, pending_verification or failed_verification."},
			"dkim_mode":      schema.StringAttribute{Computed: true, Description: dkimModeDescription},
			"rotation_ready": schema.BoolAttribute{Computed: true, Description: "Whether the domain can rotate its DKIM keys."},
			"dns_records": schema.ListNestedAttribute{
				Computed:    true,
				Description: dnsRecordsDescription,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":                      schema.StringAttribute{Computed: true},
						"hostname":                  schema.StringAttribute{Computed: true},
						"fqdn":                      schema.StringAttribute{Computed: true},
						"content":                   schema.StringAttribute{Computed: true},
						"purpose":                   schema.StringAttribute{Computed: true},
						"verification_scope":        schema.StringAttribute{Computed: true},
						"required_for_verification": schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *domainDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("domain")),
	}
}

func (d *domainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = configureDataSource(req)
}

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg domainModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := cfg.ID.ValueString()
	if id == "" {
		found, err := d.client.FindDomainByName(ctx, cfg.Domain.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Looking up Lettermint domain "+cfg.Domain.ValueString(), err.Error())
			return
		}
		id = found.ID
	}
	domain, err := d.client.GetDomain(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint domain "+id, err.Error())
		return
	}
	m, diags := domainToModel(domain)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

type routeDataSource struct {
	client *client.Client
}

type routeDataModel struct {
	ID                      types.String  `tfsdk:"id"`
	ProjectID               types.String  `tfsdk:"project_id"`
	Slug                    types.String  `tfsdk:"slug"`
	Name                    types.String  `tfsdk:"name"`
	RouteType               types.String  `tfsdk:"route_type"`
	InboundAddress          types.String  `tfsdk:"inbound_address"`
	InboundMXHostname       types.String  `tfsdk:"inbound_mx_hostname"`
	InboundDomain           types.String  `tfsdk:"inbound_domain"`
	InboundDomainVerifiedAt types.String  `tfsdk:"inbound_domain_verified_at"`
	SpamThreshold           types.Float64 `tfsdk:"spam_threshold"`
	AttachmentDelivery      types.String  `tfsdk:"attachment_delivery"`
}

func (d *routeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_route"
}

func (d *routeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, Description: desc}
	}
	resp.Schema = schema.Schema{
		Description: "Reads a Lettermint route, for example to get inbound_mx_hostname before creating the MX record for lettermint_route_inbound.",
		Attributes: map[string]schema.Attribute{
			"id":                         schema.StringAttribute{Required: true, Description: "Route id."},
			"project_id":                 computed("Project the route belongs to."),
			"slug":                       computed("Route slug."),
			"name":                       computed("Route name."),
			"route_type":                 computed("transactional, broadcast or inbound."),
			"inbound_address":            computed("The route's own inbound address."),
			"inbound_mx_hostname":        computed("Host an inbound domain's MX record must point to."),
			"inbound_domain":             computed("Custom receiving domain, or null."),
			"inbound_domain_verified_at": computed("When the inbound domain was verified, or null."),
			"spam_threshold":             schema.Float64Attribute{Computed: true, Description: "Inbound spam threshold."},
			"attachment_delivery":        computed("inline or url."),
		},
	}
}

func (d *routeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = configureDataSource(req)
}

func (d *routeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg routeDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	route, err := d.client.GetRoute(ctx, cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint route "+cfg.ID.ValueString(), err.Error())
		return
	}
	m := routeDataModel{
		ID:                      types.StringValue(route.ID),
		ProjectID:               types.StringValue(route.ProjectID),
		Slug:                    types.StringValue(route.Slug),
		Name:                    types.StringValue(route.Name),
		RouteType:               types.StringValue(route.RouteType),
		InboundAddress:          optionalString(route.InboundAddress),
		InboundMXHostname:       types.StringValue(route.InboundMXHostname),
		InboundDomain:           optionalString(route.InboundDomain),
		InboundDomainVerifiedAt: optionalString(route.InboundDomainVerifiedAt),
		SpamThreshold:           optionalFloat(route.InboundSpamThreshold),
		AttachmentDelivery:      types.StringValue(route.AttachmentDelivery),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
