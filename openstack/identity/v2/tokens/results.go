package tokens

import (
	"fmt"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/rackspace/gophercloud"
	"github.com/rackspace/gophercloud/openstack/identity/v2/tenants"
)

// Token provides only the most basic information related to an authentication token.
type Token struct {
	// ID provides the primary means of identifying a user to the OpenStack API.
	// OpenStack defines this field as an opaque value, so do not depend on its content.
	// It is safe, however, to compare for equality.
	ID string

	// ExpiresAt provides a timestamp in ISO 8601 format, indicating when the authentication token becomes invalid.
	// After this point in time, future API requests made using this authentication token will respond with errors.
	// Either the caller will need to reauthenticate manually, or more preferably, the caller should exploit automatic re-authentication.
	// See the AuthOptions structure for more details.
	ExpiresAt time.Time

	// Tenant provides information about the tenant to which this token grants access.
	Tenant tenants.Tenant
}

// Endpoint represents a single API endpoint offered by a service.
// It provides the public and internal URLs, if supported, along with a region specifier, again if provided.
// The significance of the Region field will depend upon your provider.
//
// In addition, the interface offered by the service will have version information associated with it
// through the VersionId, VersionInfo, and VersionList fields, if provided or supported.
//
// In all cases, fields which aren't supported by the provider and service combined will assume a zero-value ("").
type Endpoint struct {
	TenantID    string `mapstructure:"tenantId"`
	PublicURL   string `mapstructure:"publicURL"`
	InternalURL string `mapstructure:"internalURL"`
	AdminURL    string `mapstructure:"adminURL"`
	Region      string `mapstructure:"region"`
	VersionID   string `mapstructure:"versionId"`
	VersionInfo string `mapstructure:"versionInfo"`
	VersionList string `mapstructure:"versionList"`
}

// CatalogEntry provides a type-safe interface to an Identity API V2 service catalog listing.
// Each class of service, such as cloud DNS or block storage services, will have a single
// CatalogEntry representing it.
//
// Note: when looking for the desired service, try, whenever possible, to key off the type field.
// Otherwise, you'll tie the representation of the service to a specific provider.
type CatalogEntry struct {
	// Name will contain the provider-specified name for the service.
	Name string `mapstructure:"name"`

	// Type will contain a type string if OpenStack defines a type for the service.
	// Otherwise, for provider-specific services, the provider may assign their own type strings.
	Type string `mapstructure:"type"`

	// Endpoints will let the caller iterate over all the different endpoints that may exist for
	// the service.
	Endpoints []Endpoint `mapstructure:"endpoints"`
}

// ServiceCatalog provides a view into the service catalog from a previous, successful authentication.
type ServiceCatalog struct {
	Entries []CatalogEntry
}

// CreateResult defers the interpretation of a created token.
// Use ExtractToken() to interpret it as a Token, or ExtractServiceCatalog() to interpret it as a service catalog.
type CreateResult struct {
	gophercloud.CommonResult
}

// ExtractToken returns the just-created Token from a CreateResult.
func (result CreateResult) ExtractToken() (*Token, error) {
	if result.Err != nil {
		return nil, result.Err
	}

	var response struct {
		Access struct {
			Token struct {
				Expires string         `mapstructure:"expires"`
				ID      string         `mapstructure:"id"`
				Tenant  tenants.Tenant `mapstructure:"tenant"`
			} `mapstructure:"token"`
		} `mapstructure:"access"`
	}

	err := mapstructure.Decode(result.Resp, &response)
	if err != nil {
		return nil, err
	}

	expiresTs, err := time.Parse(gophercloud.RFC3339Milli, response.Access.Token.Expires)
	if err != nil {
		return nil, err
	}

	return &Token{
		ID:        response.Access.Token.ID,
		ExpiresAt: expiresTs,
		Tenant:    response.Access.Token.Tenant,
	}, nil
}

// ExtractServiceCatalog returns the ServiceCatalog that was generated along with the user's Token.
func (result CreateResult) ExtractServiceCatalog() (*ServiceCatalog, error) {
	if result.Err != nil {
		return nil, result.Err
	}

	var response struct {
		Access struct {
			Entries []CatalogEntry `mapstructure:"serviceCatalog"`
		} `mapstructure:"access"`
	}

	err := mapstructure.Decode(result.Resp, &response)
	if err != nil {
		return nil, err
	}

	return &ServiceCatalog{Entries: response.Access.Entries}, nil
}

// createErr quickly packs an error in a CreateResult.
func createErr(err error) CreateResult {
	return CreateResult{gophercloud.CommonResult{Err: err}}
}

// LocateEndpointURL discovers the endpoint URL for a specific service from a ServiceCatalog acquired
// from a Create request. The specified EndpointOpts are used to identify a unique, unambiguous
// endpoint to return. The minimum that can be specified is a Type, but you will also often need
// to specify a Name and/or a Region depending on what's available on your OpenStack deployment.
func LocateEndpointURL(catalog *ServiceCatalog, opts gophercloud.EndpointOpts) (string, error) {
	// Extract Endpoints from the catalog entries that match the requested Type, Name if provided, and Region if provided.
	var endpoints = make([]Endpoint, 0, 1)
	for _, entry := range catalog.Entries {
		if (entry.Type == opts.Type) && (opts.Name == "" || entry.Name == opts.Name) {
			for _, endpoint := range entry.Endpoints {
				if opts.Region == "" || endpoint.Region == opts.Region {
					endpoints = append(endpoints, endpoint)
				}
			}
		}
	}

	// Report an error if the options were ambiguous.
	if len(endpoints) == 0 {
		return "", gophercloud.ErrEndpointNotFound
	}
	if len(endpoints) > 1 {
		return "", fmt.Errorf("Discovered %d matching endpoints: %#v", len(endpoints), endpoints)
	}

	// Extract the appropriate URL from the matching Endpoint.
	for _, endpoint := range endpoints {
		switch opts.Availability {
		case gophercloud.AvailabilityPublic:
			return gophercloud.NormalizeURL(endpoint.PublicURL), nil
		case gophercloud.AvailabilityInternal:
			return gophercloud.NormalizeURL(endpoint.InternalURL), nil
		case gophercloud.AvailabilityAdmin:
			return gophercloud.NormalizeURL(endpoint.AdminURL), nil
		default:
			return "", fmt.Errorf("Unexpected availability in endpoint query: %s", opts.Availability)
		}
	}

	return "", gophercloud.ErrEndpointNotFound
}
