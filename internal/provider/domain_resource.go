package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

var (
	_ resource.Resource                = &domainResource{}
	_ resource.ResourceWithConfigure   = &domainResource{}
	_ resource.ResourceWithImportState = &domainResource{}
)

func newDomainResource() resource.Resource { return &domainResource{} }

type domainResource struct {
	client *client.Client
}

type domainModel struct {
	ID         types.String `tfsdk:"id"`
	Domain     types.String `tfsdk:"domain"`
	ProjectIDs types.Set    `tfsdk:"project_ids"`
	Status     types.String `tfsdk:"status"`
	DkimMode   types.String `tfsdk:"dkim_mode"`
	DNSRecords types.List   `tfsdk:"dns_records"`
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

const dnsRecordsDescription = "DNS records Lettermint needs for the domain, sorted by purpose and fqdn. Create them in the domain's DNS, then verify with lettermint_domain_verification."

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A sending domain in Lettermint. Import with the domain's id or its name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Domain id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				Description:   "Domain name, such as example.com. Changing it replaces the domain.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"project_ids": schema.SetAttribute{
				Description: "Projects the domain is limited to. Leave out to keep Lettermint's setting.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators:  []validator.Set{setvalidator.SizeAtLeast(1)},
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				Description: "verified, partially_verified, pending_verification or failed_verification.",
				Computed:    true,
			},
			"dkim_mode": schema.StringAttribute{
				Description:   "legacy_txt or managed_cname.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dns_records": schema.ListNestedAttribute{
				Description:   dnsRecordsDescription,
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":                      schema.StringAttribute{Computed: true, Description: "TXT, CNAME or MX."},
						"hostname":                  schema.StringAttribute{Computed: true, Description: "Name relative to the domain."},
						"fqdn":                      schema.StringAttribute{Computed: true, Description: "Fully qualified name."},
						"content":                   schema.StringAttribute{Computed: true, Description: "Record value."},
						"purpose":                   schema.StringAttribute{Computed: true, Description: "return_path, dmarc, dkim_legacy, dkim_primary or dkim_secondary."},
						"required_for_verification": schema.BoolAttribute{Computed: true, Description: "Whether the domain verifies without it."},
					},
				},
			},
		},
	}
}

func (r *domainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*client.Client)
}

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateDomain(ctx, plan.Domain.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Creating Lettermint domain "+plan.Domain.ValueString(), err.Error())
		return
	}
	// Save the id now, so a failure below does not leave an untracked domain.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), created.ID)...)

	if !plan.ProjectIDs.IsUnknown() && !plan.ProjectIDs.IsNull() {
		ids, d := stringSlice(ctx, plan.ProjectIDs)
		resp.Diagnostics.Append(d...)
		if err := r.client.SetDomainProjects(ctx, created.ID, ids); err != nil {
			resp.Diagnostics.AddError("Limiting Lettermint domain "+plan.Domain.ValueString()+" to projects", err.Error())
			return
		}
	}

	state, ok := r.read(ctx, created.ID, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// read fetches the domain into a model. ok is false on error.
func (r *domainResource) read(ctx context.Context, id string, diags *diag.Diagnostics) (*domainModel, bool) {
	d, err := r.client.GetDomain(ctx, id)
	if err != nil {
		diags.AddError("Reading Lettermint domain "+id, err.Error())
		return nil, false
	}
	m, ds := domainToModel(d)
	diags.Append(ds...)
	return m, !ds.HasError()
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	d, err := r.client.GetDomain(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint domain "+state.ID.ValueString(), err.Error())
		return
	}
	m, diags := domainToModel(d)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only project_ids can change in place; domain forces a replacement.
	if !plan.ProjectIDs.IsUnknown() && !plan.ProjectIDs.IsNull() && !plan.ProjectIDs.Equal(state.ProjectIDs) {
		ids, d := stringSlice(ctx, plan.ProjectIDs)
		resp.Diagnostics.Append(d...)
		if err := r.client.SetDomainProjects(ctx, state.ID.ValueString(), ids); err != nil {
			resp.Diagnostics.AddError("Limiting Lettermint domain "+state.Domain.ValueString()+" to projects", err.Error())
			return
		}
	}

	fresh, ok := r.read(ctx, state.ID.ValueString(), &resp.Diagnostics)
	if !ok {
		return
	}
	// Planned from state; a change shows up at the next refresh instead of
	// as an inconsistent result here.
	fresh.DkimMode = plan.DkimMode
	fresh.DNSRecords = plan.DNSRecords
	resp.Diagnostics.Append(resp.State.Set(ctx, fresh)...)
}

func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteDomain(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting Lettermint domain "+state.Domain.ValueString(), err.Error())
	}
}

// ImportState accepts the domain's id, or its name (anything with a dot).
func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID
	if strings.Contains(id, ".") {
		d, err := r.client.FindDomainByName(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Importing Lettermint domain "+id, err.Error())
			return
		}
		id = d.ID
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

func domainToModel(d *client.Domain) (*domainModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	records, ds := dnsRecordsValue(d.DNSRecords)
	diags.Append(ds...)
	projects, ds := projectIDsValue(d.Projects)
	diags.Append(ds...)
	return &domainModel{
		ID:         types.StringValue(d.ID),
		Domain:     types.StringValue(d.Domain),
		ProjectIDs: projects,
		Status:     types.StringValue(d.Status),
		DkimMode:   types.StringValue(d.DkimMode),
		DNSRecords: records,
	}, diags
}
