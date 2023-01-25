package serv

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty/v2"
)

const (
	DeployRoute   = "/api/v1/deploy"
	RollbackRoute = "/api/v1/deploy/rollback"
)

type DeployReq struct {
	Name   string `json:"name"`
	Bundle string `json:"bundle"`
}

const (
	errAuthFailed = "auth failed"
	errNotFound   = "api not found"
)

type Client struct {
	*resty.Client
}

type Resp struct {
	Msg string
}

func NewAdminClient(host string, secret string) *Client {
	c := resty.New().
		SetBaseURL(host).
		SetHeader("Content-Type", "application/json")

	if secret != "" {
		h := sha256.Sum256([]byte(secret))
		s := base64.StdEncoding.EncodeToString(h[:])
		c.SetAuthToken(s)
	}
	c.OnAfterResponse(func(c *resty.Client, res *resty.Response) error {
		var e string
		switch {
		case res.StatusCode() == 404:
			e = errNotFound
		case res.StatusCode() == 401:
			e = errAuthFailed
		case res.IsError():
			e = string(res.Body())
		}
		if e != "" {
			return errors.New(e)
		}
		return nil
	})
	return &Client{c}
}

func (c *Client) Deploy(name, confPath string) (*Resp, error) {
	errMsg := "deploy failed: %w"

	bundle, err := buildBundle(confPath)
	if err != nil {
		return nil, fmt.Errorf(errMsg, err)
	}

	res, err := c.R().
		SetBody(DeployReq{Name: name, Bundle: bundle}).
		Post(DeployRoute)
	if err != nil {
		return nil, fmt.Errorf(errMsg, err)
	}

	return &Resp{Msg: string(res.Body())}, nil
}

func (c *Client) Rollback() (*Resp, error) {
	errMsg := "rollback failed: %w"

	res, err := c.R().
		Post(RollbackRoute)
	if err != nil {
		return nil, fmt.Errorf(errMsg, err)
	}

	return &Resp{Msg: string(res.Body())}, nil
}

func buildBundle(confPath string) (string, error) {
	buf := bytes.Buffer{}
	z := zip.NewWriter(&buf)

	cpath, err := filepath.EvalSymlinks(confPath)
	if err != nil {
		return "", err
	}

	seedFile := filepath.Join(cpath, "seed.js")

	err = filepath.WalkDir(cpath, func(fp string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		bp := filepath.Base(fp)

		if bp == "migrations" {
			return filepath.SkipDir
		}

		if de.IsDir() && (de.Type()&os.ModeSymlink) == os.ModeSymlink {
			return filepath.SkipDir
		}

		if fp == "" || fp == seedFile {
			return nil
		}

		rp := strings.TrimPrefix(strings.TrimPrefix(fp, cpath), "/")
		if de.IsDir() {
			rp += "/"
		}
		zf, err := z.CreateHeader(&zip.FileHeader{Name: rp})
		if err != nil {
			return err
		}
		if de.IsDir() {
			return nil
		}

		f, err := os.Open(fp)
		if err != nil {
			return err
		}

		if _, err = io.Copy(zf, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err = z.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
