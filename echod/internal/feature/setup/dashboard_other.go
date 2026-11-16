//go:build dot

package setup

import "net/http"

// The Dot has no screen, so nothing to set for a dashboard.
func dashboardSection(http.ResponseWriter, string) {}

func saveDashboard(*http.Request) string { return "this device has no dashboard page" }

func streamDeckSection(http.ResponseWriter, string) {}

func saveStreamDeck(*http.Request) string { return "this device has no deck page" }

func findStreamDecks(*http.Request) string { return "" }

func dashboardPanelSection(http.ResponseWriter, string) {}

func saveDashboardPanel(*http.Request) string { return "this device has no dashboard page" }
