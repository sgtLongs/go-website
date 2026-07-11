package frontend

import "embed"

// Files contains the frontend pages and static assets so serving them does not
// depend on the directory from which the server is started.
//
//go:embed html/*.html assets/css/*.css assets/js/*.js
var Files embed.FS
