package email

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// PublicOrigin is the configured shop origin (PUBLIC_SHOP_ORIGIN) that every
// emailed link uses. It is never derived from Host or X-Forwarded-Host, so a
// request cannot point a link at another site.
type PublicOrigin struct{ value string }

// Link is an absolute URL on the public origin. Templates accept only Link
// values, which only PublicOrigin.Link creates.
type Link struct{ value string }

func (l Link) String() string { return l.value }

func (o PublicOrigin) String() string { return o.value }

var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ParsePublicOrigin accepts scheme://host[:port] with no userinfo, path,
// query or fragment, and returns it normalized (lowercase, default port
// removed). Production requires https; elsewhere http is allowed for local
// development. Errors never quote the value.
func ParsePublicOrigin(raw string, production bool) (PublicOrigin, error) {
	invalid := func(reason string) (PublicOrigin, error) {
		return PublicOrigin{}, fmt.Errorf("PUBLIC_SHOP_ORIGIN is invalid: %s", reason)
	}
	if raw == "" {
		return invalid("empty")
	}
	if strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "*\\%#?@ \t\r\n") {
		return invalid("must be a plain scheme://host[:port] origin")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" {
		return invalid("must be an absolute URL")
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme != "https" && scheme != "http":
		return invalid("scheme must be https")
	case production && scheme != "https":
		return invalid("scheme must be https in production")
	case u.User != nil:
		return invalid("must not contain credentials")
	case (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return invalid("must not contain a path, query or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) == nil {
		labels := strings.Split(host, ".")
		for _, label := range labels {
			if len(label) > 63 || !hostLabel.MatchString(label) {
				return invalid("host is malformed")
			}
		}
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" || strings.HasSuffix(u.Host, ":") {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return invalid("port is out of range")
		}
		if !(scheme == "https" && number == 443) && !(scheme == "http" && number == 80) {
			host += ":" + strconv.Itoa(number)
		}
	}
	return PublicOrigin{value: scheme + "://" + host}, nil
}

var (
	linkPath     = regexp.MustCompile(`^/[a-z0-9][a-z0-9/-]*$`)
	linkFragment = regexp.MustCompile(`^[A-Za-z0-9_-]+=[A-Za-z0-9_-]+$`)
)

// ErrInvalidLink rejects a path or fragment outside the fixed link shapes.
var ErrInvalidLink = errors.New("invalid email link")

// Link builds origin + path, with an optional fragment such as "t=<token>".
// Fragments are never sent to servers, so a token in one does not reach
// access logs or Referer headers.
func (o PublicOrigin) Link(path, fragment string) (Link, error) {
	if o.value == "" {
		return Link{}, fmt.Errorf("%w: no public origin", ErrInvalidLink)
	}
	if !linkPath.MatchString(path) || strings.Contains(path, "//") {
		return Link{}, fmt.Errorf("%w: path", ErrInvalidLink)
	}
	if fragment != "" && !linkFragment.MatchString(fragment) {
		return Link{}, fmt.Errorf("%w: fragment", ErrInvalidLink)
	}
	value := o.value + path
	if fragment != "" {
		value += "#" + fragment
	}
	return Link{value: value}, nil
}
