package core

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/dosco/graphjin/internal/jsn"
	"github.com/mitchellh/mapstructure"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// RemoteAPI struct defines a remote API endpoint
type remoteAPI struct {
	URL   string
	Debug bool

	PassHeaders []string `mapstructure:"pass_headers"`
	SetHeaders  []struct {
		Name  string
		Value string
	} `mapstructure:"set_headers"`
}

func newRemoteAPI(v map[string]interface{}) (*remoteAPI, error) {
	ra := &remoteAPI{}
	if err := mapstructure.Decode(v, ra); err != nil {
		return nil, err
	}
	return ra, nil
}

func (r *remoteAPI) Resolve(c context.Context, rr ResolverReq) ([]byte, error) {
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

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

	res, err := client.Do(req)
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

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if err := jsn.ValidateBytes(b); err != nil {
		return nil, err
	}

	return b, nil
}
