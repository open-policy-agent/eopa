package utils

import (
	"net/http"

	rhttp "github.com/hashicorp/go-retryablehttp"
)

var StdlibHTTPClient = http.DefaultClient

func init() {
	http.DefaultClient = rhttp.NewClient().StandardClient()
}
