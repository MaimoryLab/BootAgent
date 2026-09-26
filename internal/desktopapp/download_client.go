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
// Some CDNs redirect to a presigned URL whose query carries the asset's own file
// name with an unencoded space (a ModelScope-backed mirror once used for a
// desktop download did exactly this: ?filename=Some App-2.0.1.dmg&auth_key=...).
// net/http keeps URL.RawQuery verbatim when it writes the request target, so the
// space goes out inside the request line, where HTTP has no way to read it as
// anything but the end of the target, and the origin answers 400. curl escapes
// the space before sending, which is why such a URL reproduces fine by hand.
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
