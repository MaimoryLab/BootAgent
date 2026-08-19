package desktopapp

import (
	"fmt"
	"net/http"
	"strings"
)

// maxDownloadRedirects matches net/http's own default. Setting CheckRedirect
// replaces that default, so the limit has to be restated or a redirect loop
// would follow forever.
const maxDownloadRedirects = 10

// downloadRedirectClient follows redirects the way an installer download needs.
//
// The mirror for DSH Desktop redirects to ModelScope, which redirects again to a
// presigned CDN URL whose query carries the asset's own file name:
//
//	?filename=DSH Desktop-2.0.1-universal.dmg&...&auth_key=...
//
// That space is unencoded in the Location header. net/http keeps URL.RawQuery
// verbatim when it writes the request target, so the space goes out inside the
// request line, where HTTP has no way to read it as anything but the end of the
// target. The CDN's Tengine answered "400 Bad Request" and every mirror install
// failed at the last hop -- reported as "download returned HTTP 400", which
// looked like a network or region problem and so survived being tried with and
// without a VPN. curl escapes the space before sending, which is why the same
// URL reproduced fine by hand.
//
// Repairing the redirect target rather than the parsed URL keeps this to the one
// thing that is wrong: the path already survives, because URL.String escapes it.
var downloadRedirectClient = &http.Client{
	CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= maxDownloadRedirects {
			return fmt.Errorf("stopped after %d redirects", maxDownloadRedirects)
		}
		request.URL.RawQuery = encodeQuerySpaces(request.URL.RawQuery)
		return nil
	},
}

// encodeQuerySpaces percent-encodes spaces a server left raw in a redirect
// target. Only spaces are touched: they are what terminates the request target,
// and rebuilding the whole query would re-encode the signature bytes a
// presigned URL is validated on.
func encodeQuerySpaces(query string) string {
	if !strings.Contains(query, " ") {
		return query
	}
	return strings.ReplaceAll(query, " ", "%20")
}
