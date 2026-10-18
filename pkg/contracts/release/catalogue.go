package release

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var ErrCatalogue = errors.New("release catalogue must be a configured HTTPS directory")

func CatalogueURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || strings.ContainsAny(u.Path, "\\\x00") {
		return nil, ErrCatalogue
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, ErrCatalogue
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, ErrCatalogue
		}
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, ErrCatalogue
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	return u, nil
}
