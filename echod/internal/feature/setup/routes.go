package setup

import "net/http"

// routes are pages of the setup page's own beyond the ones serve names, each in the file it belongs
// to: added here, rather than as more cases there, so a page added in one place does not have to be
// threaded through another.
var routes = map[string]func(f *Feature, w http.ResponseWriter, r *http.Request){}
