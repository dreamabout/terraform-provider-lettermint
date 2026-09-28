package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

var (
	_ resource.Resource                = &domainVerificationResource{}
	_ resource.ResourceWithConfigure   = &domainVerificationResource{}
	_ resource.ResourceWithImportState = &domainVerificationResource{}
)

const defaultVerifyTimeout = 10 * time.Minute

func newDomainVerificationResource() resource.Resource { return &domainVerificationResource{} }

type domainVerificationResource struct {
	client *client.Client
}

type domainVerificationModel struct {
	ID       types.String   `tfsdk:"id"`
	DomainID types.String   `tfsdk:"domain_id"`
	Status   types.String   `tfsdk:"status"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *domainVerificationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_verification"
}

func (r *domainVerificationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Waits until Lettermint has verified a domain's DNS records. Create it after the records from lettermint_domain.dns_records exist in DNS. " +
			"If Lettermint later reports the domain as pending or failed, the resource is planned again and the next apply re-verifies. Destroying it changes nothing in Lettermint.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Same as domain_id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain_id": schema.StringAttribute{
				Description:   "Id of the lettermint_domain to verify.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Description:   "The domain's status after verification: verified, or partially_verified when only recommended records are missing.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *domainVerificationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*client.Client)
}

func (r *domainVerificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainVerificationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, defaultVerifyTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.DomainID.ValueString()
	domain, err := r.client.GetDomain(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint domain "+id, err.Error())
		return
	}

	var last *client.DomainVerification
	err = poll(ctx, timeout, func(ctx context.Context) (bool, error) {
		res, err := r.client.VerifyDomain(ctx, id)
		if err != nil {
			return false, err
		}
		last = res
		return res.Verified, nil
	})
	if errors.Is(err, errPollTimeout) {
		resp.Diagnostics.AddError(
			"Lettermint domain "+domain.Domain+" not verified",
			fmt.Sprintf("Domain %s was not verified within %s.%s", domain.Domain, timeout, failedRecordsDetail(last)),
		)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Verifying Lettermint domain "+domain.Domain, err.Error())
		return
	}
	if len(last.RecommendedFailedRecords) > 0 {
		resp.Diagnostics.AddWarning(
			"Lettermint domain "+domain.Domain+" verified without recommended records",
			"Recommended records that did not verify:"+recordList(last.RecommendedFailedRecords),
		)
	}

	domain, err = r.client.GetDomain(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint domain "+id, err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	plan.Status = types.StringValue(domain.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func failedRecordsDetail(v *client.DomainVerification) string {
	if v == nil {
		return ""
	}
	detail := ""
	if v.Message != "" {
		detail += " Lettermint: " + v.Message
	}
	if len(v.FailedRecords) > 0 {
		detail += "\n\nRecords that did not verify:" + recordList(v.FailedRecords)
	}
	return detail
}

func recordList(records []client.VerificationRecord) string {
	var b strings.Builder
	for _, r := range records {
		fmt.Fprintf(&b, "\n  - %s %s", r.Type, r.Name)
	}
	return b.String()
}

func (r *domainVerificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainVerificationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	d, err := r.client.GetDomain(ctx, state.DomainID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading Lettermint domain "+state.DomainID.ValueString(), err.Error())
		return
	}
	switch d.Status {
	case "pending_verification", "failed_verification":
		// No longer verified: plan it again so the next apply re-verifies.
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = state.DomainID
	state.Status = types.StringValue(d.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only sees timeouts change; everything else replaces.
func (r *domainVerificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan domainVerificationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *domainVerificationResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *domainVerificationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), req.ID)...)
}
