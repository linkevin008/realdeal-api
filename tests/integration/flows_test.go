//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	c := &client{t: t}
	resp := c.do("GET", "/health", nil).mustStatus(t, http.StatusOK)
	if resp.body["status"] != "healthy" {
		t.Fatalf("unexpected health response: %v", resp.body)
	}
}

func TestAuthFlow(t *testing.T) {
	email := fmt.Sprintf("it-auth-%d@example.com", time.Now().UnixNano())
	c := &client{t: t}

	c.do("POST", "/api/v1/auth/signup", map[string]interface{}{
		"name":     "Auth Tester",
		"email":    email,
		"password": "password123",
	}).mustStatus(t, http.StatusCreated)

	signin := c.do("POST", "/api/v1/auth/signin", map[string]interface{}{
		"email":    email,
		"password": "password123",
	}).mustStatus(t, http.StatusOK)
	c.token = signin.data(t)["access_token"].(string)

	me := c.do("GET", "/api/v1/users/me", nil).mustStatus(t, http.StatusOK)
	if got := me.data(t)["email"]; got != email {
		t.Fatalf("GET /users/me returned email %v, want %s", got, email)
	}
}

// createListing creates an active listing carrying a marker in its description
// so search queries can isolate this test run's data (the address stays
// realistic — markers in the street made dev/staging browse screens ugly).
// The listing is soft-deleted on test cleanup so persistent databases (dev
// volume, RDS when run as a smoke test) don't accumulate visible test data.
func createListing(t *testing.T, seller *client, marker string, price float64, beds int) string {
	t.Helper()
	resp := seller.do("POST", "/api/v1/properties", map[string]interface{}{
		"street":        fmt.Sprintf("%d Maple St", int(price/1000)),
		"city":          "Springfield",
		"state":         "IL",
		"country":       "US",
		"price":         price,
		"property_type": "house",
		"bedrooms":      beds,
		"description":   "integration test listing " + marker,
		"latitude":      39.78,
		"longitude":     -89.65,
	}).mustStatus(t, http.StatusCreated)
	id := resp.data(t)["id"].(string)
	t.Cleanup(func() {
		seller.do("DELETE", "/api/v1/properties/"+id, nil)
	})
	return id
}

// searchTotal queries the lookup service through the gateway and returns the
// listings matching the marker.
func searchListings(t *testing.T, c *client, query string) []interface{} {
	t.Helper()
	resp := c.do("GET", "/api/v1/search/properties?"+query, nil).mustStatus(t, http.StatusOK)
	return resp.dataList(t)
}

// TestSearchAndOfferLifecycle is the cross-service flow: listings written via
// core become searchable via lookup, and an accepted offer (core) removes the
// listing from search results (lookup) — write side and read side agree.
func TestSearchAndOfferLifecycle(t *testing.T) {
	seller, _ := signup(t, "seller")
	marker := fmt.Sprintf("IT%d", time.Now().UnixNano())

	cheap := createListing(t, seller, marker, 100000, 2)
	createListing(t, seller, marker, 200000, 3)
	createListing(t, seller, marker, 300000, 4)

	// Lookup sees what core wrote, in the requested order
	results := searchListings(t, seller, "q="+marker+"&sort=price_asc")
	if len(results) != 3 {
		t.Fatalf("search returned %d listings, want 3: %v", len(results), results)
	}
	first := results[0].(map[string]interface{})
	if first["id"] != cheap {
		t.Fatalf("sort=price_asc: first result is %v, want cheapest %s", first["id"], cheap)
	}

	// Filters narrow the same data
	if got := len(searchListings(t, seller, "q="+marker+"&min_price=150000")); got != 2 {
		t.Fatalf("min_price filter returned %d listings, want 2", got)
	}
	if got := len(searchListings(t, seller, "q="+marker+"&beds=4")); got != 1 {
		t.Fatalf("beds filter returned %d listings, want 1", got)
	}

	// Two buyers bid on the cheapest listing
	buyer1, _ := signup(t, "buyer")
	buyer2, _ := signup(t, "buyer")
	offerPath := "/api/v1/properties/" + cheap + "/offers"
	offer1 := buyer1.do("POST", offerPath, map[string]interface{}{"amount": 105000}).
		mustStatus(t, http.StatusCreated).data(t)["id"].(string)
	buyer2.do("POST", offerPath, map[string]interface{}{"amount": 110000}).
		mustStatus(t, http.StatusCreated)

	// Seller sees both offers and accepts buyer1's
	offers := seller.do("GET", offerPath, nil).mustStatus(t, http.StatusOK).dataList(t)
	if len(offers) != 2 {
		t.Fatalf("seller sees %d offers, want 2", len(offers))
	}
	seller.do("PUT", offerPath+"/"+offer1+"/accept", nil).mustStatus(t, http.StatusOK)

	// Competing offer was auto-rejected
	myOffers := buyer2.do("GET", "/api/v1/users/me/offers", nil).mustStatus(t, http.StatusOK).dataList(t)
	rejected := false
	for _, o := range myOffers {
		offer := o.(map[string]interface{})
		if offer["property_id"] == cheap && offer["status"] == "rejected" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatalf("buyer2's offer was not auto-rejected: %v", myOffers)
	}

	// The accepted listing leaves the search results (status active -> pending)
	if got := len(searchListings(t, seller, "q="+marker+"&sort=price_asc")); got != 2 {
		t.Fatalf("after acceptance search returned %d listings, want 2", got)
	}
}
