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
	"path"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty/v2"
)

const (
	errAuthFailed = "auth failed"
	errNotFound   = "api not found"
)

type Client struct {
	*resty.Client
}

func NewClient(host string, secret string) *Client {
	c := resty.New().SetHostURL(host)
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

func (c *Client) Deploy(name, confPath string) error {
	errMsg := "deploy failed: %w"

	bundle, err := buildBundle(confPath)
	if err != nil {
		return fmt.Errorf(errMsg, err)
	}

	_, err = c.R().
		SetHeader("Content-Type", "application/json").
		SetBody(deployReq{name, bundle}).
		Post(deployRoute)

	if err != nil {
		return fmt.Errorf(errMsg, err)
	}

	return nil
}

func (c *Client) Rollback() error {
	errMsg := "rollback failed: %w"

	_, err := c.R().
		SetHeader("Content-Type", "application/json").
		Post(rollbackRoute)

	if err != nil {
		return fmt.Errorf(errMsg, err)
	}

	return nil
}

func buildBundle(confPath string) (string, error) {
	buf := bytes.Buffer{}
	z := zip.NewWriter(&buf)

	cpath, err := filepath.EvalSymlinks(confPath)
	if err != nil {
		return "", err
	}

	seedFile := path.Join(cpath, "seed.js")

	err = filepath.WalkDir(cpath, func(fp string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() || (de.Type()&os.ModeSymlink) == os.ModeSymlink {
			bp := filepath.Base(fp)
			if bp == "migrations" {
				return filepath.SkipDir
			}
			return nil
		}

		if fp == "" || fp == seedFile {
			return nil
		}

		relPath := strings.TrimPrefix(fp, cpath)
		zf, err := z.Create(relPath)
		if err != nil {
			return err
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
