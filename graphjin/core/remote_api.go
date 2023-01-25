package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/jsn"
)

// RemoteAPI struct defines a remote API endpoint
type remoteAPI struct {
	httpClient *http.Client
	URL        string
	Debug      bool

	PassHeaders []string
	SetHeaders  []remoteHdrs
}

type remoteHdrs struct {
	Name  string
	Value string
}

func newRemoteAPI(v map[string]interface{}, httpClient *http.Client) (*remoteAPI, error) {
	ra := remoteAPI{
		httpClient: httpClient,
	}

	if v, ok := v["url"].(string); ok {
		ra.URL = v
	}
	if v, ok := v["debug"].(bool); ok {
		ra.Debug = v
	}
	if v, ok := v["pass_headers"].([]string); ok {
		ra.PassHeaders = v
	}
	if v, ok := v["set_headers"].(map[string]string); ok {
		for k, v1 := range v {
			rh := remoteHdrs{Name: k, Value: v1}
			ra.SetHeaders = append(ra.SetHeaders, rh)
		}
	}

	return &ra, nil
}

func (r *remoteAPI) Resolve(c context.Context, rr ResolverReq) ([]byte, error) {
	uri := strings.ReplaceAll(r.URL, "$id", rr.ID)

	req, err := http.NewRequestWithContext(c, "GET", uri, nil)
	if err != nil {
		return nil, err
	}

	// if host, ok := hdr["Host"]; ok {
	// 	req.Host = host[0]
	// }

	for _, v := range r.SetHeaders {
		req.Header.Set(v.Name, v.Value)
	}

	// for _, v := range r.PassHeaders {
	// 	req.Header.Set(v, hdr.Get(v))
	// }

	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to '%s': %v", uri, err)
	}
	defer res.Body.Close()

	if r.Debug {
		reqDump, err := httputil.DumpRequestOut(req, true)
		if err != nil {
			return nil, err
		}

		resDump, err := httputil.DumpResponse(res, true)
		if err != nil {
			return nil, err
		}

		rr.Log.Printf("DBG Remote Request:\n%s\n%s",
			reqDump, resDump)
	}

	if res.StatusCode != 200 {
		return nil,
			fmt.Errorf("server responded with a %d", res.StatusCode)
	}

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if err := jsn.ValidateBytes(b); err != nil {
		return nil, err
	}

	return b, nil
}
