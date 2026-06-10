package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SupportedCountries is the single source of truth for where listings can be
// created. The iOS client fetches this list to populate its country dropdown,
// and CreateProperty validates against it — add a code here (ISO 3166-1
// alpha-2) to open a new market, and every consumer picks it up.
var SupportedCountries = []string{"US", "CA"}

var supportedCountrySet = func() map[string]bool {
	set := make(map[string]bool, len(SupportedCountries))
	for _, c := range SupportedCountries {
		set[c] = true
	}
	return set
}()

// SupportedCountriesHandler godoc
// GET /api/v1/config/countries
// Public. Returns the ISO codes listings may be created in; clients localize
// the display names themselves.
func SupportedCountriesHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": SupportedCountries})
}
