package gitea

import (
	"io"
	"net/http"
	"strings"

	"github.com/42wim/caddy-gitea/pkg/gitea"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func init() {
	caddy.RegisterModule(Middleware{})
	httpcaddyfile.RegisterHandlerDirective("gitea", parseCaddyfile)
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Middleware
	err := m.UnmarshalCaddyfile(h.Dispenser)

	return m, err
}

// Middleware implements gitea plugin.
type Middleware struct {
	Client             *gitea.Client `json:"-"`
	Server             string        `json:"server,omitempty"`
	Token              string        `json:"token,omitempty"`
	GiteaPages         string        `json:"gitea_pages,omitempty"`
	GiteaPagesAllowAll string        `json:"gitea_pages_allowall,omitempty"`
	Domain             string        `json:"domain,omitempty"`
}

// CaddyModule returns the Caddy module information.
func (Middleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.gitea",
		New: func() caddy.Module { return new(Middleware) },
	}
}

// Provision provisions gitea client.
func (m *Middleware) Provision(ctx caddy.Context) error {
	var err error
	m.Client, err = gitea.NewClient(m.Server, m.Token, m.GiteaPages, m.GiteaPagesAllowAll)

	return err
}

// Validate implements caddy.Validator.
func (m *Middleware) Validate() error {
	return nil
}

// UnmarshalCaddyfile unmarshals a Caddyfile.
func (m *Middleware) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for n := d.Nesting(); d.NextBlock(n); {
			switch d.Val() {
			case "server":
				d.Args(&m.Server)
			case "token":
				d.Args(&m.Token)
			case "gitea_pages":
				d.Args(&m.GiteaPages)
			case "gitea_pages_allowall":
				d.Args(&m.GiteaPagesAllowAll)
			case "domain":
				d.Args(&m.Domain)
			}
		}
	}

	return nil
}

// ServeHTTP performs gitea content fetcher.
func (m Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	var owner, repo, filePath string

	owner, repo, filePath, ref := m.inferOwnerRepoPathAndRef(r)

	f, err := m.Client.Open(owner, repo, filePath, ref, m.Domain == "")
	if err != nil {
		return caddyhttp.Error(http.StatusNotFound, err)
	}

	_, err = io.Copy(w, f)

	return err
}

func (m Middleware) inferOwnerRepoPathAndRef(r *http.Request) (owner, repo, filePath, ref string) {
	// remove the domain if it's set (works fine if it's empty)
	// if we haven't specified a domain, do not support repo.username and branch.repo.username
	host := strings.TrimRight(strings.TrimSuffix(r.Host, m.Domain), ".")
	h := strings.Split(host, ".")

	owner = h[0]
	ref = r.URL.Query().Get("ref")

	// This maintains the legacy behavior from github.com/42wim/caddy-gitea
	// that allows the repo to be specified as the first part of the path,
	// but prevents subdirectories from being hosted.
	if m.Domain == "" {
		parts := strings.Split(r.URL.Path, "/")
		switch {
		case len(parts) == 1:
			repo = ""
			filePath = parts[0]
		case len(parts) > 1:
			repo = parts[0]
			filePath = strings.Join(parts[1:], "/")
		default:
		}
	} else {
		// This is the new behavior which is closer to what you may
		// know from Github Pages.
		//
		// There are three cases for the Host value:
		// Given                          Inferred
		// <owner>.domain                 ref=<default>, repo=<default>
		// <repo>.<owner>.domain          ref=<default>
		// <branch>.<repo>.<owner>.domain
		//
		// In all cases, filepath is simply URL.Path.

		switch {
		case len(h) == 1:
			owner = h[0]
			repo = ""
		case len(h) == 2:
			owner = h[1]
			repo = h[0]
		case len(h) == 3:
			owner = h[2]
			repo = h[1]
			ref = h[0]
		}
		filePath = r.URL.Path
	}
	return owner, repo, filePath, ref
}

// Interface guards
var (
	_ caddy.Provisioner           = (*Middleware)(nil)
	_ caddy.Validator             = (*Middleware)(nil)
	_ caddyhttp.MiddlewareHandler = (*Middleware)(nil)
	_ caddyfile.Unmarshaler       = (*Middleware)(nil)
)
