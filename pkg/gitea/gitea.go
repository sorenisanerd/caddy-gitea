package gitea

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"

	gclient "code.gitea.io/sdk/gitea"
	"github.com/spf13/viper"
)

type AllowedBranches int

const (
	AllowedBranchesNone AllowedBranches = iota
	AllowedBranchesLimited
	AllowedBranchesAll
)

type Client struct {
	serverURL          string
	token              string
	giteapages         string
	giteapagesAllowAll string
	gc                 *gclient.Client
}

func NewClient(serverURL, token, giteapages, giteapagesAllowAll string) (*Client, error) {
	if giteapages == "" {
		giteapages = "gitea-pages"
	}

	if giteapagesAllowAll == "" {
		giteapagesAllowAll = "gitea-pages-allowall"
	}

	gc, err := gclient.NewClient(serverURL, gclient.SetToken(token), gclient.SetGiteaVersion(""))
	if err != nil {
		return nil, err
	}

	return &Client{
		serverURL:          serverURL,
		token:              token,
		gc:                 gc,
		giteapages:         giteapages,
		giteapagesAllowAll: giteapagesAllowAll,
	}, nil
}

func (c *Client) Open(owner, repo string, filepath *string, ref string, compatibilityMode bool) (fs.File, error) {
	// if repo is empty they want to have the gitea-pages repo
	if repo == "" {
		repo = c.giteapages
	}

	// if filepath is empty they want to have the index.html
	if *filepath == "" || *filepath == "/" {
		*filepath = "index.html"
	}

	// we need to check if the repo exists (and allows access)
	allowedBranches := c.allowsPages(owner, repo)
	if allowedBranches == AllowedBranchesNone {
		// if we're checking the default repo (usually
		// "gitea-pages") and the desired branch doesn't
		// exist, return 404
		if repo == c.giteapages && !c.hasRepoBranch(owner, repo, c.giteapages) {
			return nil, fs.ErrNotExist
		}

		if compatibilityMode {
			// this is for the compatibility thing where the path
			// *may* have a repo name in it, but in this case the
			// first part of the path did not represent a valid
			// repo, so we try the default repo
			maybeRepo := c.giteapages

			if ref == "" {
				ref = c.giteapages
			}

			allowedBranches2 := c.allowsPages(owner, maybeRepo)
			if allowedBranches2 == AllowedBranchesNone || !c.hasRepoBranch(owner, repo, c.giteapages) {
				return nil, fs.ErrNotExist
			}
		}
	}

	hasConfig := true

	if err := c.readConfig(owner, repo); err != nil {
		// we don't need a config for gitea-pages
		// no config is only exposing the gitea-pages branch
		if repo != c.giteapages && (allowedBranches < AllowedBranchesAll) {
			return nil, err
		}

		hasConfig = false
	}

	// if we don't have a config and the repo is the gitea-pages
	// always overwrite the ref to the gitea-pages branch
	if !hasConfig && (repo == c.giteapages || ref == c.giteapages) {
		ref = c.giteapages
	} else if !validRefs(ref, allowedBranches) {
		return nil, fs.ErrNotExist
	}

	res, err := c.getRawFileOrLFS(owner, repo, *filepath, ref)
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(*filepath, ".md") {
		res, err = handleMD(res)
		if err != nil {
			return nil, err
		}
	}

	return &openFile{
		content: res,
		name:    *filepath,
	}, nil
}

func (c *Client) getRawFileOrLFS(owner, repo, filepath, ref string) ([]byte, error) {
	var (
		giteaURL string
		err      error
	)

	// TODO: make pr for go-sdk
	// gitea sdk doesn't support "media" type for lfs/non-lfs
	giteaURL, err = url.JoinPath(c.serverURL+"/api/v1/repos/", owner, repo, "media", url.QueryEscape(filepath))
	if err != nil {
		return nil, err
	}

	giteaURL += "?ref=" + url.QueryEscape(ref)

	req, err := http.NewRequest(http.MethodGet, giteaURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "token "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, fs.ErrNotExist
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("unexpected status code '%d'", resp.StatusCode)
	}

	res, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	return res, nil
}

var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func handleMD(res []byte) ([]byte, error) {
	meta, resbody, err := extractFrontMatter(string(res))
	if err != nil {
		return nil, err
	}

	resmd, err := markdown([]byte(resbody))
	if err != nil {
		return nil, err
	}

	title, _ := meta["title"].(string)

	res = append([]byte("<!DOCTYPE html>\n<html>\n<body>\n<h1>"), []byte(title)...)
	res = append(res, []byte("</h1>")...)
	res = append(res, resmd...)
	res = append(res, []byte("</body></html>")...)

	return res, nil
}

func (c *Client) repoTopics(owner, repo string) ([]string, error) {
	repos, _, err := c.gc.ListRepoTopics(owner, repo, gclient.ListRepoTopicsOptions{})
	return repos, err
}

func (c *Client) hasRepoBranch(owner, repo, branch string) bool {
	b, _, err := c.gc.GetRepoBranch(owner, repo, branch)
	if err != nil {
		return false
	}

	return b.Name == branch
}

func (c *Client) allowsPages(owner, repo string) AllowedBranches {
	topics, err := c.repoTopics(owner, repo)
	if err != nil {
		return AllowedBranchesNone
	}

	for _, topic := range topics {
		if topic == c.giteapagesAllowAll {
			return AllowedBranchesAll
		}
	}

	for _, topic := range topics {
		if topic == c.giteapages {
			return AllowedBranchesLimited
		}
	}

	return AllowedBranchesNone
}

func (c *Client) readConfig(owner, repo string) error {
	cfg, err := c.getRawFileOrLFS(owner, repo, c.giteapages+".toml", c.giteapages)
	if err != nil {
		return err
	}

	viper.SetConfigType("toml")

	return viper.ReadConfig(bytes.NewBuffer(cfg))
}

func validRefs(ref string, allowedBranches AllowedBranches) bool {
	if allowedBranches == AllowedBranchesAll {
		return true
	}

	validrefs := viper.GetStringSlice("allowedrefs")
	for _, r := range validrefs {
		if r == ref {
			return true
		}

		if r == "*" {
			return true
		}
	}

	return false
}
