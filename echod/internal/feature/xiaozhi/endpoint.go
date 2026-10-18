package xiaozhi

import "github.com/HuskerMinion/techo5/echod/internal/lib/endpoint"

// manualEndpointSettings allows a pause between phrases without changing the
// endpoint policy of the other voice backend. Windows can exercise this policy
// without importing the Linux audio drivers.
func manualEndpointSettings() endpoint.Settings {
	settings := endpoint.Default
	settings.Quiet = 12 // 1.2 seconds at 16 kHz, in 1600-sample windows.
	return settings
}
