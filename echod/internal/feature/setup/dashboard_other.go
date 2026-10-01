//go:build dot

package setup

import (
	"net/http"
	"slices"
)

// The Dot has no screen, so nothing to set for a dashboard.
func dashboardSection(http.ResponseWriter, string) {}

func saveDashboard(*http.Request) string { return "this device has no dashboard page" }

func deckSection(http.ResponseWriter, string) {}

func saveDeck(*http.Request) string { return "this device has no deck page" }

func findDecks(*http.Request) string { return "" }

// A Dot has no screen to show photos on, so no Photos tab.
func init() {
	tabs = slices.DeleteFunc(tabs, func(t tab) bool { return t.id == "photos" })
}
