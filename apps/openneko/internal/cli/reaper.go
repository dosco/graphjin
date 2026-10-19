package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	reaperInterval   = 30 * time.Second
	containerGrace   = 30 * time.Second
	imageGrace       = 24 * time.Hour
	dockerSocketPath = "/var/run/docker.sock"
	librarianRepo    = "ghcr.io/open-neko/neko-librarian"
	// pluginBaseRepo is the legacy plugin sandbox image. Plugins were
	// removed, but hosts upgraded from older releases can still carry its
	// stopped sandboxes and images, so the reaper keeps recognizing them.
	pluginBaseRepo     = "ghcr.io/open-neko/plugin-base"
	managedByOpenShell = "openshell.ai/managed-by"
)

// The OpenShell Docker driver can leave a stopped container behind when its
// parent exits. Docker retains both its JSON log and its image until that
// container is removed. Keep this process separate from the web/worker: only
// it gets access to the host Docker socket.
func newReaperCmd() *cobra.Command {
	var once bool
	var check bool
	cmd := &cobra.Command{
		Use:    "reaper",
		Short:  "Reap exited OpenNeko sandboxes and unused old images",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reaper := newDockerReaper(dockerSocketPath)
			if check {
				_, err := reaper.request(cmd.Context(), http.MethodGet, "/_ping", nil)
				return err
			}
			sweep := func() {
				ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
				defer cancel()
				containers, images, err := reaper.sweep(ctx, time.Now())
				if containers+images > 0 || err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "reaper: removed %d exited sandboxes and %d old images", containers, images)
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "; %v", err)
					}
					fmt.Fprintln(cmd.ErrOrStderr())
				}
			}
			if once {
				ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
				defer cancel()
				containers, images, err := reaper.sweep(ctx, time.Now())
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %d exited sandboxes and %d old images.\n", containers, images)
				return err
			}
			sweep()
			ticker := time.NewTicker(reaperInterval)
			defer ticker.Stop()
			for {
				select {
				case <-cmd.Context().Done():
					return nil
				case <-ticker.C:
					sweep()
				}
			}
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "Run one cleanup pass and exit")
	cmd.Flags().BoolVar(&check, "check", false, "Check access to the Docker daemon")
	return cmd
}

type dockerReaper struct {
	client *http.Client
	base   string
}

func newDockerReaper(socket string) *dockerReaper {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &dockerReaper{client: &http.Client{Transport: transport}, base: "http://docker"}
}

func (r *dockerReaper) request(ctx context.Context, method, path string, result any) (int, error) {
	return r.requestBody(ctx, method, path, nil, result)
}

func (r *dockerReaper) requestBody(ctx context.Context, method, path string, body []byte, result any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return resp.StatusCode, fmt.Errorf("Docker %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(message)))
	}
	if result != nil {
		return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(result)
	}
	return resp.StatusCode, nil
}

type dockerContainerSummary struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	ImageID string   `json:"ImageID"`
	State   string   `json:"State"`
}

type dockerContainerDetail struct {
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status     string    `json:"Status"`
		FinishedAt time.Time `json:"FinishedAt"`
	} `json:"State"`
	HostConfig struct {
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
}

type dockerImageSummary struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Created     int64    `json:"Created"`
}

func openNekoAgentSandbox(name, image, sandboxName string) bool {
	if !strings.HasPrefix(name, "/openshell-") {
		return false
	}
	// Recent OpenShell Docker drivers create from an image ID. OpenNeko's
	// distinct sandbox name identifies those containers without relying on a
	// human-readable image reference in Docker's inspect response.
	for _, prefix := range []string{"openneko-warm-", "openneko-work-", "openneko-job-", "neko-p-", "neko-w-", "neko-j-"} {
		if strings.HasPrefix(sandboxName, prefix) {
			return true
		}
	}
	// Earlier installations used generic sandbox names; require our exact
	// agent repository as a second ownership check for those containers.
	if !strings.HasPrefix(image, "ghcr.io/open-neko/agent:") && !strings.HasPrefix(image, "ghcr.io/open-neko/agent@") {
		return false
	}
	for _, prefix := range []string{"/openshell-warm-", "/openshell-work-", "/openshell-job-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func openNekoPluginSandbox(name, image, imageID string, ownedImages map[string]bool) bool {
	if !strings.HasPrefix(name, "/openshell-") {
		return false
	}
	return strings.HasPrefix(image, pluginBaseRepo+":") ||
		strings.HasPrefix(image, pluginBaseRepo+"@") || ownedImages[imageID]
}

func openNekoPluginImageIDs(images []dockerImageSummary) map[string]bool {
	owned := make(map[string]bool)
	for _, image := range images {
		for _, ref := range append(append([]string{}, image.RepoTags...), image.RepoDigests...) {
			if strings.HasPrefix(ref, pluginBaseRepo+":") || strings.HasPrefix(ref, pluginBaseRepo+"@") {
				owned[image.ID] = true
			}
		}
	}
	return owned
}

func oldLibrarianDigest(image dockerImageSummary, referenced map[string]bool, now time.Time) bool {
	if image.ID == "" || referenced[image.ID] || now.Sub(time.Unix(image.Created, 0)) < imageGrace || len(image.RepoDigests) == 0 {
		return false
	}
	for _, tag := range image.RepoTags {
		// Docker's containerd image store also reports digest-only references
		// in RepoTags. They display as <none> in `docker image ls` but are not
		// removed by `docker image prune`.
		if tag != "<none>:<none>" && !strings.HasPrefix(tag, librarianRepo+"@sha256:") {
			return false
		}
	}
	for _, digest := range image.RepoDigests {
		if !strings.HasPrefix(digest, librarianRepo+"@sha256:") {
			return false
		}
	}
	return true
}

func oldAgentImage(image dockerImageSummary, referenced map[string]bool, now time.Time, currentRef string) bool {
	return oldVersionedImage(image, referenced, now, "ghcr.io/open-neko/agent", currentRef)
}

func oldVersionedImage(image dockerImageSummary, referenced map[string]bool, now time.Time, repo, currentRef string) bool {
	if currentRef == "" || image.ID == "" || referenced[image.ID] || now.Sub(time.Unix(image.Created, 0)) < imageGrace {
		return false
	}
	found := false
	for _, tag := range image.RepoTags {
		if tag == currentRef || strings.HasPrefix(tag, currentRef+"@sha256:") {
			return false
		}
		if strings.HasPrefix(tag, repo+":") || strings.HasPrefix(tag, repo+"@sha256:") {
			found = true
			continue
		}
		if tag != "<none>:<none>" {
			return false
		}
	}
	for _, digest := range image.RepoDigests {
		if !strings.HasPrefix(digest, repo+"@sha256:") {
			return false
		}
		found = true
	}
	return found
}

func oldOpenShellImage(image dockerImageSummary, referenced map[string]bool, now time.Time, currentVersion string) bool {
	if currentVersion == "" {
		return false
	}
	for _, repo := range []string{
		"ghcr.io/nvidia/openshell/gateway",
		"ghcr.io/nvidia/openshell/sandbox",
		"ghcr.io/nvidia/openshell/supervisor",
	} {
		if oldVersionedImage(image, referenced, now, repo, repo+":"+currentVersion) {
			return true
		}
	}
	return false
}

func currentAgentImageRef() string {
	if ref := strings.TrimSpace(os.Getenv("OPENNEKO_AGENT_IMAGE")); ref != "" {
		return ref
	}
	if version := strings.TrimSpace(os.Getenv("OPENNEKO_VERSION")); version != "" {
		return "ghcr.io/open-neko/agent:" + version
	}
	return ""
}

func (r *dockerReaper) sweep(ctx context.Context, now time.Time) (int, int, error) {
	var containers []dockerContainerSummary
	if _, err := r.request(ctx, http.MethodGet, "/containers/json?all=1", &containers); err != nil {
		return 0, 0, err
	}
	var images []dockerImageSummary
	_, imageErr := r.request(ctx, http.MethodGet, "/images/json?all=1", &images)
	pluginImages := openNekoPluginImageIDs(images)
	referenced := make(map[string]bool, len(containers))
	removedContainers := 0
	var failures []error
	for _, container := range containers {
		referenced[container.ImageID] = true
		if len(container.Names) == 0 || !strings.HasPrefix(container.Names[0], "/openshell-") {
			continue
		}
		var detail dockerContainerDetail
		status, err := r.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(container.ID)+"/json", &detail)
		if status == http.StatusNotFound {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if detail.Config.Labels[managedByOpenShell] != "openshell" ||
			(!openNekoAgentSandbox(detail.Name, detail.Config.Image, detail.Config.Labels["openshell.ai/sandbox-name"]) &&
				!openNekoPluginSandbox(detail.Name, detail.Config.Image, container.ImageID, pluginImages)) {
			continue
		}
		if detail.State.Status != "exited" && detail.HostConfig.RestartPolicy.Name != "no" && detail.HostConfig.RestartPolicy.Name != "" {
			// The old Docker driver gave sandbox containers an automatic
			// restart policy. A failed agent could therefore grow its JSON log
			// indefinitely without ever becoming eligible for removal.
			_, err = r.requestBody(ctx, http.MethodPost, "/containers/"+url.PathEscape(container.ID)+"/update", []byte(`{"RestartPolicy":{"Name":"no"}}`), nil)
			if err != nil {
				failures = append(failures, err)
			}
		}
		if detail.State.Status != "exited" || detail.State.FinishedAt.IsZero() ||
			now.Sub(detail.State.FinishedAt) < containerGrace {
			continue
		}
		status, err = r.request(ctx, http.MethodDelete, "/containers/"+url.PathEscape(container.ID)+"?v=false&force=false", nil)
		if status == http.StatusNotFound || status == http.StatusConflict {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		removedContainers++
	}
	if imageErr != nil {
		return removedContainers, 0, errors.Join(append(failures, imageErr)...)
	}
	removedImages := 0
	currentAgent := currentAgentImageRef()
	currentOpenShell := strings.TrimSpace(os.Getenv("OPENSHELL_VERSION"))
	for _, image := range images {
		if !oldLibrarianDigest(image, referenced, now) && !oldAgentImage(image, referenced, now, currentAgent) &&
			!oldOpenShellImage(image, referenced, now, currentOpenShell) {
			continue
		}
		// Delete references individually. Docker can reject deletion by image ID
		// when a release left both a tag and a digest attached to the image.
		refs := make(map[string]bool)
		for _, ref := range append(append([]string{}, image.RepoTags...), image.RepoDigests...) {
			if ref != "" && ref != "<none>:<none>" {
				refs[ref] = true
			}
		}
		removed := false
		for ref := range refs {
			status, err := r.request(ctx, http.MethodDelete, "/images/"+url.PathEscape(ref)+"?force=false&noprune=false", nil)
			if status == http.StatusNotFound || status == http.StatusConflict {
				continue
			}
			if err != nil {
				failures = append(failures, err)
				continue
			}
			removed = true
		}
		if removed {
			removedImages++
		}
	}
	return removedContainers, removedImages, errors.Join(failures...)
}

// Called before the upgrade pull as well as by the long-running reaper. An
// unsuccessful sweep is advisory: it must never stop an otherwise valid stack.
func reapBeforePull(ctx context.Context) {
	reaper := newDockerReaper(dockerSocketPath)
	sweepCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	containers, images, err := reaper.sweep(sweepCtx, time.Now())
	if containers+images > 0 {
		fmt.Fprintf(os.Stderr, "Reaped %d exited sandboxes and %d old images before pull.\n", containers, images)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: pre-pull artifact cleanup failed: %v\n", err)
	}
}
