package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Route is a Lettermint route (RouteData).
type Route struct {
	ID                      string   `json:"id"`
	ProjectID               string   `json:"project_id"`
	Slug                    string   `json:"slug"`
	Name                    string   `json:"name"`
	RouteType               string   `json:"route_type"`
	InboundAddress          *string  `json:"inbound_address"`
	InboundMXHostname       string   `json:"inbound_mx_hostname"`
	InboundDomain           *string  `json:"inbound_domain"`
	InboundDomainVerifiedAt *string  `json:"inbound_domain_verified_at"`
	InboundSpamThreshold    *float64 `json:"inbound_spam_threshold"`
	AttachmentDelivery      string   `json:"attachment_delivery"`
}

// InboundSettings is an update of a route's inbound settings. Nil fields are
// left out of the request and keep their value.
type InboundSettings struct {
	Domain *string
	// ClearDomain sends inbound_domain as null, removing the custom domain.
	ClearDomain        bool
	SpamThreshold      *float64
	AttachmentDelivery *string
}

// InboundVerification is the outcome of one inbound domain check.
type InboundVerification struct {
	Verified          bool
	Message           string
	ExpectedMXRecords []string
	FoundMXRecords    []string
}

// GetRoute returns a route.
func (c *Client) GetRoute(ctx context.Context, id string) (*Route, error) {
	var r Route
	if err := c.Do(ctx, http.MethodGet, "/routes/"+url.PathEscape(id), nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateRouteInbound changes an inbound route's settings.
func (c *Client) UpdateRouteInbound(ctx context.Context, id string, s InboundSettings) error {
	inbound := map[string]any{}
	switch {
	case s.ClearDomain:
		inbound["inbound_domain"] = nil
	case s.Domain != nil:
		inbound["inbound_domain"] = *s.Domain
	}
	if s.SpamThreshold != nil {
		inbound["inbound_spam_threshold"] = *s.SpamThreshold
	}
	if s.AttachmentDelivery != nil {
		inbound["attachment_delivery"] = *s.AttachmentDelivery
	}
	body := map[string]any{"inbound_settings": inbound}
	return c.Do(ctx, http.MethodPut, "/routes/"+url.PathEscape(id), body, nil)
}

// VerifyInboundDomain asks Lettermint to check the route's inbound domain MX
// records once. A failed check is not an error: it is reported in the result.
func (c *Client) VerifyInboundDomain(ctx context.Context, id string) (*InboundVerification, error) {
	status, body, err := c.send(ctx, http.MethodPost, "/routes/"+url.PathEscape(id)+"/verify-inbound-domain", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusUnprocessableEntity {
		return nil, newAPIError(status, body)
	}
	var payload struct {
		Data struct {
			Verified          bool            `json:"verified"`
			Message           string          `json:"message"`
			ExpectedMXRecords []string        `json:"expected_mx_records"`
			FoundMXRecords    json.RawMessage `json:"found_mx_records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, newAPIError(status, body)
	}
	res := &InboundVerification{
		Verified:          status == http.StatusOK && payload.Data.Verified,
		Message:           payload.Data.Message,
		ExpectedMXRecords: payload.Data.ExpectedMXRecords,
	}
	// found_mx_records is a string or a list of strings.
	var one string
	if json.Unmarshal(payload.Data.FoundMXRecords, &one) == nil {
		res.FoundMXRecords = []string{one}
	} else {
		_ = json.Unmarshal(payload.Data.FoundMXRecords, &res.FoundMXRecords)
	}
	return res, nil
}
