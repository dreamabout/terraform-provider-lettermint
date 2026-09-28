package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Domain is a sending domain (DomainData).
type Domain struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Status   string `json:"status"`
	DkimMode string `json:"dkim_mode"`
	// RotationReady is true when the domain can rotate its DKIM keys
	// (managed_cname).
	RotationReady bool            `json:"rotation_ready"`
	DNSRecords    []DNSRecord     `json:"dns_records"`
	Projects      []DomainProject `json:"projects"`
}

// DNSRecord is a record Lettermint needs in the domain's DNS
// (DomainDnsRecordData).
type DNSRecord struct {
	ID                      string `json:"id"`
	Type                    string `json:"type"`
	Hostname                string `json:"hostname"`
	FQDN                    string `json:"fqdn"`
	Content                 string `json:"content"`
	Status                  string `json:"status"`
	Purpose                 string `json:"purpose"`
	VerificationScope       string `json:"verification_scope"`
	RequiredForVerification bool   `json:"required_for_verification"`
}

// DomainProject is a project a domain is limited to.
type DomainProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DomainVerification is the outcome of one verification attempt.
type DomainVerification struct {
	// Verified is true when every record required for verification passed.
	Verified                 bool
	Message                  string
	FailedRecords            []VerificationRecord
	RecommendedFailedRecords []VerificationRecord
}

// VerificationRecord names a record that did not verify.
type VerificationRecord struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// CreateDomain adds a domain to the team.
func (c *Client) CreateDomain(ctx context.Context, name string) (*Domain, error) {
	var d Domain
	if err := c.Do(ctx, http.MethodPost, "/domains", map[string]string{"domain": name}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDomain returns a domain with its DNS records, projects and status.
func (c *Client) GetDomain(ctx context.Context, id string) (*Domain, error) {
	var d Domain
	path := "/domains/" + url.PathEscape(id) + "?include=dnsRecords,projects"
	if err := c.Do(ctx, http.MethodGet, path, nil, &d); err != nil {
		return nil, err
	}
	// DomainData in OpenAPI 0.0.1 has no status; the list does.
	if d.Status == "" {
		listed, err := c.FindDomainByName(ctx, d.Domain)
		if err != nil && !IsNotFound(err) {
			return nil, err
		}
		if listed != nil {
			d.Status = listed.Status
		}
	}
	return &d, nil
}

// FindDomainByName returns the domain whose name matches exactly (ignoring
// case), as listed (without DNS records). It returns a 404 APIError if there
// is none.
func (c *Client) FindDomainByName(ctx context.Context, name string) (*Domain, error) {
	domains, err := c.listDomains(ctx, name)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		if strings.EqualFold(domains[i].Domain, name) {
			return &domains[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "no domain named " + name}
}

// ListDomains returns every domain in the team, as listed (without DNS
// records).
func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
	return c.listDomains(ctx, "")
}

// listDomains follows the cursor through all pages. filter, when set, is
// Lettermint's partial, case-insensitive match on the name.
func (c *Client) listDomains(ctx context.Context, filter string) ([]Domain, error) {
	var all []Domain
	cursor := ""
	for {
		q := url.Values{}
		if filter != "" {
			q.Set("filter[domain]", filter)
		}
		q.Set("page[size]", "100")
		if cursor != "" {
			q.Set("page[cursor]", cursor)
		}
		var page struct {
			Data       []Domain `json:"data"`
			NextCursor *string  `json:"next_cursor"`
		}
		status, body, err := c.send(ctx, http.MethodGet, "/domains?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status > 299 {
			return nil, newAPIError(status, body)
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("lettermint: decode GET /domains: %w", err)
		}
		all = append(all, page.Data...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			return all, nil
		}
		cursor = *page.NextCursor
	}
}

// SetDomainProjects limits the domain to the given projects (at least one).
func (c *Client) SetDomainProjects(ctx context.Context, id string, projectIDs []string) error {
	body := map[string][]string{"project_ids": projectIDs}
	return c.Do(ctx, http.MethodPut, "/domains/"+url.PathEscape(id)+"/projects", body, nil)
}

// DeleteDomain removes the domain from the team.
func (c *Client) DeleteDomain(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/domains/"+url.PathEscape(id), nil, nil)
}

// VerifyDomain asks Lettermint to check the domain's DNS records once. A
// failed check is not an error: it is reported in the result.
func (c *Client) VerifyDomain(ctx context.Context, id string) (*DomainVerification, error) {
	path := "/domains/" + url.PathEscape(id) + "/dns-records/verify"
	status, body, err := c.send(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusUnprocessableEntity {
		return nil, newAPIError(status, body)
	}
	var payload struct {
		Message                  string               `json:"message"`
		FailedRecords            []VerificationRecord `json:"failed_records"`
		RecommendedFailedRecords []VerificationRecord `json:"recommended_failed_records"`
	}
	if err := json.Unmarshal(body, &payload); err != nil && status == http.StatusUnprocessableEntity {
		return nil, newAPIError(status, body)
	}
	return &DomainVerification{
		Verified:                 status == http.StatusOK,
		Message:                  payload.Message,
		FailedRecords:            payload.FailedRecords,
		RecommendedFailedRecords: payload.RecommendedFailedRecords,
	}, nil
}
