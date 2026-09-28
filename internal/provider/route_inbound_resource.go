package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

var (
	_ resource.Resource                = &routeInboundResource{}
	_ resource.ResourceWithConfigure   = &routeInboundResource{}
	_ resource.ResourceWithImportState = &routeInboundResource{}
	_ resource.ResourceWithModifyPlan  = &routeInboundResource{}
)

func newRouteInboundResource() resource.Resource { return &routeInboundResource{} }

type routeInboundResource struct {
	client *client.Client
}

type routeInboundModel struct {
	ID                      types.String   `tfsdk:"id"`
	RouteID                 types.String   `tfsdk:"route_id"`
	InboundDomain           types.String   `tfsdk:"inbound_domain"`
	SpamThreshold           types.Float64  `tfsdk:"spam_threshold"`
	AttachmentDelivery      types.String   `tfsdk:"attachment_delivery"`
	Verify                  types.Bool     `tfsdk:"verify"`
	InboundMXHostname       types.String   `tfsdk:"inbound_mx_hostname"`
	InboundAddress          types.String   `tfsdk:"inbound_address"`
	InboundDomainVerifiedAt types.String   `tfsdk:"inbound_domain_verified_at"`
	Timeouts                timeouts.Value `tfsdk:"timeouts"`
}

func (r *routeInboundResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_route_inbound"
}

func (r *routeInboundResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Inbound settings of an existing inbound route: its custom receiving domain, spam threshold and attachment delivery. " +
			"The route itself is neither created nor deleted; destroying the resource clears inbound_domain. Import with the route's id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Same as route_id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"route_id": schema.StringAttribute{
				Description:   "Id of an inbound route.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"inbound_domain": schema.StringAttribute{
				Description: "Custom receiving domain, such as support.example.com or *.m.example.com. Leave out to clear it.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"spam_threshold": schema.Float64Attribute{
				Description:   "Inbound spam threshold, 0 to 10. Higher is more permissive. Leave out to keep Lettermint's value.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.Float64{float64validator.Between(0, 10)},
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
			},
			"attachment_delivery": schema.StringAttribute{
				Description:   "inline or url. Leave out to keep Lettermint's value.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.OneOf("inline", "url")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"verify": schema.BoolAttribute{
				Description: "Wait until Lettermint has verified the inbound domain's MX records. Create the MX record (inbound_mx_hostname, or the lettermint_route data source) first. Defaults to true.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"inbound_mx_hostname": schema.StringAttribute{
				Description:   "Host the inbound domain's MX record must point to.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"inbound_address": schema.StringAttribute{
				Description:   "The route's own inbound address.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"inbound_domain_verified_at": schema.StringAttribute{
				Description: "When Lettermint verified the inbound domain, or null.",
				Computed:    true,
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Update: true}),
		},
	}
}

func (r *routeInboundResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*client.Client)
}

// ModifyPlan keeps inbound_domain_verified_at when neither the domain nor
// verify changes, and knows it is null when there is no domain.
func (r *routeInboundResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan routeInboundModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.InboundDomain.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("inbound_domain_verified_at"), types.StringNull())...)
		return
	}
	if req.State.Raw.IsNull() {
		return
	}
	var state routeInboundModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.InboundDomain.Equal(state.InboundDomain) && plan.Verify.Equal(state.Verify) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("inbound_domain_verified_at"), state.InboundDomainVerifiedAt)...)
	}
}

func (r *routeInboundResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan routeInboundModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, defaultVerifyTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := plan.RouteID.ValueString()

	route, err := r.client.GetRoute(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint route "+id, err.Error())
		return
	}
	if route.RouteType != "inbound" {
		resp.Diagnostics.AddAttributeError(path.Root("route_id"), "Not an inbound route",
			fmt.Sprintf("route %s is a %s route; lettermint_route_inbound needs an inbound route.", id, route.RouteType))
		return
	}

	r.apply(ctx, &plan, timeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *routeInboundResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan routeInboundModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, defaultVerifyTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, timeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// apply writes the planned inbound settings, verifies the domain when asked,
// and fills the computed attributes of m from the route.
func (r *routeInboundResource) apply(ctx context.Context, m *routeInboundModel, timeout time.Duration, diags *diag.Diagnostics) {
	id := m.RouteID.ValueString()

	settings := client.InboundSettings{}
	if m.InboundDomain.IsNull() {
		settings.ClearDomain = true
	} else {
		v := m.InboundDomain.ValueString()
		settings.Domain = &v
	}
	if !m.SpamThreshold.IsUnknown() && !m.SpamThreshold.IsNull() {
		v := m.SpamThreshold.ValueFloat64()
		settings.SpamThreshold = &v
	}
	if !m.AttachmentDelivery.IsUnknown() && !m.AttachmentDelivery.IsNull() {
		v := m.AttachmentDelivery.ValueString()
		settings.AttachmentDelivery = &v
	}
	if err := r.client.UpdateRouteInbound(ctx, id, settings); err != nil {
		diags.AddError("Updating inbound settings of Lettermint route "+id, err.Error())
		return
	}

	route, err := r.client.GetRoute(ctx, id)
	if err != nil {
		diags.AddError("Reading Lettermint route "+id, err.Error())
		return
	}

	if m.Verify.ValueBool() && route.InboundDomain != nil && route.InboundDomainVerifiedAt == nil {
		var last *client.InboundVerification
		err := poll(ctx, timeout, func(ctx context.Context) (bool, error) {
			res, err := r.client.VerifyInboundDomain(ctx, id)
			if err != nil {
				return false, err
			}
			last = res
			return res.Verified, nil
		})
		if errors.Is(err, errPollTimeout) {
			diags.AddError("Lettermint inbound domain "+*route.InboundDomain+" not verified",
				fmt.Sprintf("Inbound domain %s on route %s was not verified within %s.%s", *route.InboundDomain, id, timeout, mxDetail(last)))
			return
		}
		if err != nil {
			diags.AddError("Verifying inbound domain of Lettermint route "+id, err.Error())
			return
		}
		if route, err = r.client.GetRoute(ctx, id); err != nil {
			diags.AddError("Reading Lettermint route "+id, err.Error())
			return
		}
	}

	routeToModel(route, m)
}

func mxDetail(v *client.InboundVerification) string {
	if v == nil {
		return ""
	}
	found := "none"
	if len(v.FoundMXRecords) > 0 {
		found = strings.Join(v.FoundMXRecords, ", ")
	}
	return fmt.Sprintf(" Lettermint expects MX records pointing to %s; found: %s.", strings.Join(v.ExpectedMXRecords, ", "), found)
}

func routeToModel(route *client.Route, m *routeInboundModel) {
	m.ID = types.StringValue(route.ID)
	m.RouteID = types.StringValue(route.ID)
	m.InboundDomain = keepCase(m.InboundDomain, optionalString(route.InboundDomain))
	m.SpamThreshold = optionalFloat(route.InboundSpamThreshold)
	m.AttachmentDelivery = types.StringValue(route.AttachmentDelivery)
	m.InboundMXHostname = types.StringValue(route.InboundMXHostname)
	m.InboundAddress = optionalString(route.InboundAddress)
	m.InboundDomainVerifiedAt = optionalString(route.InboundDomainVerifiedAt)
	if route.AttachmentDelivery == "" {
		m.AttachmentDelivery = types.StringNull()
	}
}

func (r *routeInboundResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state routeInboundModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	route, err := r.client.GetRoute(ctx, state.RouteID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint route "+state.RouteID.ValueString(), err.Error())
		return
	}
	routeToModel(route, &state)
	if state.Verify.IsNull() {
		// After import.
		state.Verify = types.BoolValue(true)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *routeInboundResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state routeInboundModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.RouteID.ValueString()
	err := r.client.UpdateRouteInbound(ctx, id, client.InboundSettings{ClearDomain: true})
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Clearing inbound domain of Lettermint route "+id, err.Error())
	}
}

func (r *routeInboundResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("route_id"), req.ID)...)
}
