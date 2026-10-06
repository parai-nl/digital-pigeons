//go:build skipauth

package duif

// The existing framework dev build injects a synthetic administrator without
// a cookie. Production cannot reach this branch; there is no environment flag.
const recheckCookieAccess = false
