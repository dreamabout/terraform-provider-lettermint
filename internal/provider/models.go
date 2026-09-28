package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dreamabout/terraform-provider-lettermint/internal/client"
)

// dnsRecordAttrTypes are the attributes of one element of dns_records. Only
// fields that describe the record are exposed, not its verification status,
// so verifying a domain does not change the plan.
var dnsRecordAttrTypes = map[string]attr.Type{
	"type":                      types.StringType,
	"hostname":                  types.StringType,
	"fqdn":                      types.StringType,
	"content":                   types.StringType,
	"purpose":                   types.StringType,
	"required_for_verification": types.BoolType,
}

var dnsRecordObjectType = types.ObjectType{AttrTypes: dnsRecordAttrTypes}

// dnsRecordsValue converts records to dns_records, sorted by purpose, fqdn
// and type so the order does not depend on the API.
func dnsRecordsValue(records []client.DNSRecord) (types.List, diag.Diagnostics) {
	sorted := append([]client.DNSRecord(nil), records...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Purpose != b.Purpose {
			return a.Purpose < b.Purpose
		}
		if a.FQDN != b.FQDN {
			return a.FQDN < b.FQDN
		}
		return a.Type < b.Type
	})

	var diags diag.Diagnostics
	elems := make([]attr.Value, 0, len(sorted))
	for _, r := range sorted {
		obj, d := types.ObjectValue(dnsRecordAttrTypes, map[string]attr.Value{
			"type":                      types.StringValue(r.Type),
			"hostname":                  types.StringValue(r.Hostname),
			"fqdn":                      types.StringValue(r.FQDN),
			"content":                   types.StringValue(r.Content),
			"purpose":                   types.StringValue(r.Purpose),
			"required_for_verification": types.BoolValue(r.RequiredForVerification),
		})
		diags.Append(d...)
		elems = append(elems, obj)
	}
	list, d := types.ListValue(dnsRecordObjectType, elems)
	diags.Append(d...)
	return list, diags
}

func projectIDsValue(projects []client.DomainProject) (types.Set, diag.Diagnostics) {
	elems := make([]attr.Value, 0, len(projects))
	for _, p := range projects {
		elems = append(elems, types.StringValue(p.ID))
	}
	return types.SetValue(types.StringType, elems)
}

func stringSlice(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	var out []string
	diags := set.ElementsAs(ctx, &out, false)
	return out, diags
}

func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// keepCase returns prior when Lettermint returned the same name in another
// case, so a normalized name does not show up as a change.
func keepCase(prior types.String, fromAPI types.String) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && !fromAPI.IsNull() && strings.EqualFold(prior.ValueString(), fromAPI.ValueString()) {
		return prior
	}
	return fromAPI
}

func optionalFloat(f *float64) types.Float64 {
	if f == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*f)
}

// pollInterval is how long to wait between verification attempts. Tests
// shorten it.
var pollInterval = 15 * time.Second

// errPollTimeout is returned by poll when the timeout ran out.
var errPollTimeout = fmt.Errorf("timed out")

// poll calls attempt until it reports done, returns an error, or timeout has
// passed. attempt always runs at least once.
func poll(ctx context.Context, timeout time.Duration, attempt func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		done, err := attempt(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return errPollTimeout
			}
			return err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return errPollTimeout
		case <-time.After(pollInterval):
		}
	}
}
