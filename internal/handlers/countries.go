package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Subdivision is a first-level administrative division (US state, Canadian
// province/territory). Codes are ISO 3166-2 second parts; names are served
// here because platform locale APIs don't cover subdivisions.
type Subdivision struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// SupportedCountry pairs a country code with the subdivisions a listing
// address may use. An empty subdivision list means free-text entry.
type SupportedCountry struct {
	Code         string        `json:"code"`
	Subdivisions []Subdivision `json:"subdivisions"`
}

// SupportedCountries is the single source of truth for where listings can be
// created and which state/province codes are valid there. The iOS client
// fetches this to build its country and state/province dropdowns, and
// CreateProperty validates against it — extend this list to open a market.
var SupportedCountries = []SupportedCountry{
	{Code: "US", Subdivisions: []Subdivision{
		{"AL", "Alabama"}, {"AK", "Alaska"}, {"AZ", "Arizona"}, {"AR", "Arkansas"},
		{"CA", "California"}, {"CO", "Colorado"}, {"CT", "Connecticut"}, {"DE", "Delaware"},
		{"DC", "District of Columbia"}, {"FL", "Florida"}, {"GA", "Georgia"}, {"HI", "Hawaii"},
		{"ID", "Idaho"}, {"IL", "Illinois"}, {"IN", "Indiana"}, {"IA", "Iowa"},
		{"KS", "Kansas"}, {"KY", "Kentucky"}, {"LA", "Louisiana"}, {"ME", "Maine"},
		{"MD", "Maryland"}, {"MA", "Massachusetts"}, {"MI", "Michigan"}, {"MN", "Minnesota"},
		{"MS", "Mississippi"}, {"MO", "Missouri"}, {"MT", "Montana"}, {"NE", "Nebraska"},
		{"NV", "Nevada"}, {"NH", "New Hampshire"}, {"NJ", "New Jersey"}, {"NM", "New Mexico"},
		{"NY", "New York"}, {"NC", "North Carolina"}, {"ND", "North Dakota"}, {"OH", "Ohio"},
		{"OK", "Oklahoma"}, {"OR", "Oregon"}, {"PA", "Pennsylvania"}, {"RI", "Rhode Island"},
		{"SC", "South Carolina"}, {"SD", "South Dakota"}, {"TN", "Tennessee"}, {"TX", "Texas"},
		{"UT", "Utah"}, {"VT", "Vermont"}, {"VA", "Virginia"}, {"WA", "Washington"},
		{"WV", "West Virginia"}, {"WI", "Wisconsin"}, {"WY", "Wyoming"},
	}},
	{Code: "CA", Subdivisions: []Subdivision{
		{"AB", "Alberta"}, {"BC", "British Columbia"}, {"MB", "Manitoba"},
		{"NB", "New Brunswick"}, {"NL", "Newfoundland and Labrador"}, {"NS", "Nova Scotia"},
		{"NT", "Northwest Territories"}, {"NU", "Nunavut"}, {"ON", "Ontario"},
		{"PE", "Prince Edward Island"}, {"QC", "Quebec"}, {"SK", "Saskatchewan"},
		{"YT", "Yukon"},
	}},
}

// supportedCountrySet and subdivisionSets are derived lookups for validation.
var supportedCountrySet, subdivisionSets = func() (map[string]bool, map[string]map[string]bool) {
	countries := make(map[string]bool, len(SupportedCountries))
	subs := make(map[string]map[string]bool, len(SupportedCountries))
	for _, c := range SupportedCountries {
		countries[c.Code] = true
		if len(c.Subdivisions) == 0 {
			continue
		}
		set := make(map[string]bool, len(c.Subdivisions))
		for _, s := range c.Subdivisions {
			set[s.Code] = true
		}
		subs[c.Code] = set
	}
	return countries, subs
}()

// supportedCountryCodes renders the codes for error messages.
func supportedCountryCodes() []string {
	codes := make([]string, len(SupportedCountries))
	for i, c := range SupportedCountries {
		codes[i] = c.Code
	}
	return codes
}

// SupportedCountriesHandler godoc
// GET /api/v1/config/countries
// Public. Returns the countries listings may be created in, each with its
// valid state/province codes and display names.
func SupportedCountriesHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": SupportedCountries})
}
