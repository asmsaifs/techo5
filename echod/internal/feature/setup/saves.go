package setup

import "net/http"

// saves are forms of the setup page's own beyond the ones save names, each in the file it belongs to: one
// added here rather than as another case there, so a form added in one place does not have to be threaded
// through another. Each answers what was wrong, or nothing.
var saves = map[string]func(*http.Request) string{}
