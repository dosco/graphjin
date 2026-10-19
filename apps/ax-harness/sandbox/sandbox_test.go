package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func startSandbox(t *testing.T, switchUsers bool) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	workspaces := filepath.Join(root, "workspaces")
	workspace := filepath.Join(workspaces, "org-1")
	if err := os.MkdirAll(workspace, 0o777); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(workspace, 0o777)
	_ = os.Chmod(root, 0o755)
	_ = os.Chmod(filepath.Dir(root), 0o755)
	server := &Server{Workspaces: workspaces, Homes: filepath.Join(root, "homes"), BaseUID: 23000, Users: 2, GID: os.Getgid(), SwitchUsers: switchUsers, PATH: "/usr/bin:/bin"}
	socket := filepath.Join(root, "sandbox.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx, socket) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return server, socket, workspace
}

func TestSandboxRunsCommandsWithoutSecretsAsTheRunUser(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	switchUsers := os.Geteuid() == 0
	server, socket, workspace := startSandbox(t, switchUsers)
	client := NewClient(socket, workspace)
	ctx := context.Background()

	result, err := client.Exec(ctx, "id -u; env; echo hi > out.txt", "", 10, 10_000)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("exec = %+v, %v", result, err)
	}
	if strings.Contains(result.Output, "must-not-leak") || strings.Contains(result.Output, "OPENAI_API_KEY") {
		t.Fatalf("the sandbox passed a secret to the command:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "HOME="+filepath.Join(server.Homes, client.run)) {
		t.Fatalf("the run has no home of its own:\n%s", result.Output)
	}
	if switchUsers {
		if first := strings.SplitN(result.Output, "\n", 2)[0]; first != strconv.Itoa(server.BaseUID) {
			t.Fatalf("command ran as uid %s, want %d", first, server.BaseUID)
		}
		info, err := os.Stat(filepath.Join(workspace, "out.txt"))
		if err != nil || info.Mode().Perm()&0o020 == 0 {
			t.Fatalf("a file the run wrote is not group-writable: %v %v", info.Mode(), err)
		}
	}

	other := NewClient(socket, workspace)
	if result, _ := other.Exec(ctx, "id -u", "", 10, 10_000); switchUsers && strings.TrimSpace(result.Output) == strconv.Itoa(server.BaseUID) {
		t.Fatal("two runs share a user")
	}

	if _, err := NewClient(socket, "/etc").Exec(ctx, "true", "", 10, 10_000); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("a workspace outside the sandbox was admitted: %v", err)
	}

	if _, err := client.Exec(ctx, "sleep 60 >/dev/null 2>&1 &", "", 10, 10_000); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if switchUsers && len(processesOf(server.BaseUID)) > 0 {
		t.Fatal("ending the run left its background process running")
	}
	if _, err := os.Stat(filepath.Join(server.Homes, client.run)); !os.IsNotExist(err) {
		t.Fatalf("ending the run left its home: %v", err)
	}
	if _, err := other.Exec(ctx, "true", "", 10, 10_000); err != nil {
		t.Fatal(err)
	}
	third := NewClient(socket, workspace)
	if _, err := third.Exec(ctx, "true", "", 10, 10_000); err != nil {
		t.Fatalf("an ended run's user was not freed: %v", err)
	}
}

func TestSandboxSocketBelongsToTheSharedGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can hand the socket to another group")
	}
	root := t.TempDir()
	server := &Server{Workspaces: root, Homes: filepath.Join(root, "homes"), BaseUID: 23000, Users: 1, GID: 23999, SwitchUsers: true, PATH: "/usr/bin:/bin"}
	socket := filepath.Join(root, "sandbox.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx, socket) }()
	defer func() {
		cancel()
		<-done
	}()
	var info os.FileInfo
	for i := 0; i < 100; i++ {
		if found, err := os.Stat(socket); err == nil && found.Sys().(*syscall.Stat_t).Gid == 23999 {
			info = found
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if info == nil {
		t.Fatal("the socket was not handed to the shared group")
	}
	if perm := info.Mode().Perm(); perm != 0o660 {
		t.Fatalf("socket mode = %o, want 660", perm)
	}
}

func processesOf(uid int) []int {
	var pids []int
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if err != nil || strings.Contains(string(status), "State:\tZ") {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if fields := strings.Fields(line); len(fields) > 1 && fields[0] == "Uid:" && fields[1] == strconv.Itoa(uid) {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// TestSandboxKeepsRunsApart stages two runs as the launcher does: owned by
// the host, shared with its group, closed to others. Each run's user reaches
// only its own stage, and what it writes stays readable by the shared group.
func TestSandboxKeepsRunsApart(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("switching run users needs root")
	}
	const shared = 4242
	root := t.TempDir()
	_ = os.Chmod(root, 0o755)
	_ = os.Chmod(filepath.Dir(root), 0o755)
	workspaces := filepath.Join(root, "runs")
	stage := func(name string) string {
		dir := filepath.Join(workspaces, name, "workspace", "org")
		if err := os.MkdirAll(dir, 0o770); err != nil {
			t.Fatal(err)
		}
		for p := dir; p != workspaces; p = filepath.Dir(p) {
			_ = os.Chown(p, 0, shared)
			_ = os.Chmod(p, 0o2770)
		}
		return dir
	}
	_ = os.MkdirAll(workspaces, 0o770)
	_ = os.Chown(workspaces, 0, shared)
	_ = os.Chmod(workspaces, 0o2770)
	a, b := stage("owner.boot.aaaa"), stage("owner.boot.bbbb")
	if err := os.WriteFile(filepath.Join(b, "secret.txt"), []byte("run b only"), 0o660); err != nil {
		t.Fatal(err)
	}
	_ = os.Chown(filepath.Join(b, "secret.txt"), 0, shared)

	server := &Server{Workspaces: workspaces, Homes: filepath.Join(root, "homes"), BaseUID: 23100, Users: 4, GID: shared, SwitchUsers: true, PATH: "/usr/bin:/bin"}
	socket := filepath.Join(root, "sandbox.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx, socket) }()
	t.Cleanup(func() { cancel(); <-done })
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	runA, runB := NewClient(socket, a), NewClient(socket, b)
	res, err := runA.Exec(context.Background(), "id -G; echo made > mine.txt; cat mine.txt", "", 10, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if errors.Is(err, errNoACL) || strings.Contains(res.Output, "POSIX ACL") {
		t.Skip("this filesystem has no POSIX ACLs")
	}
	if res.ExitCode != 0 || !strings.Contains(res.Output, "made") {
		t.Fatalf("run A could not use its own stage: %+v", res)
	}
	if groups := strings.Fields(strings.SplitN(res.Output, "\n", 2)[0]); len(groups) != 1 || groups[0] == strconv.Itoa(shared) {
		t.Fatalf("run A's groups = %v, want only its own", groups)
	}
	if _, err := runB.Exec(context.Background(), "true", "", 10, 10_000); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"cat " + filepath.Join(b, "secret.txt"), "ls " + workspaces, "touch " + filepath.Join(b, "planted")} {
		res, err := runA.Exec(context.Background(), cmd, "", 10, 10_000)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode == 0 {
			t.Fatalf("run A could %q in another run's stage: %s", cmd, res.Output)
		}
	}
	// What run A wrote carries the shared group's grant, so the host can
	// copy it back.
	acl := make([]byte, 256)
	n, err := syscall.Getxattr(filepath.Join(a, "mine.txt"), "system.posix_acl_access", acl)
	if err != nil {
		t.Fatalf("run A's file has no ACL: %v", err)
	}
	if !aclHasGroup(acl[:n], shared) {
		t.Fatal("run A's file is not shared with the host's group")
	}
	if !aclHasGroup(acl[:n], server.BaseUID) {
		t.Fatal("run A's file is not granted to run A's group")
	}
	// Ending the run takes its group off the stage, so a later run that gets
	// the same ID cannot reach it.
	if err := runA.Close(); err != nil {
		t.Fatal(err)
	}
	n, err = syscall.Getxattr(filepath.Join(a, "mine.txt"), "system.posix_acl_access", acl)
	if err != nil || aclHasGroup(acl[:n], server.BaseUID) || !aclHasGroup(acl[:n], shared) {
		t.Fatalf("ending run A left its group on the stage (err %v)", err)
	}
}

func aclHasGroup(acl []byte, gid int) bool {
	for i := 4; i+8 <= len(acl); i += 8 {
		tag := uint16(acl[i]) | uint16(acl[i+1])<<8
		id := uint32(acl[i+4]) | uint32(acl[i+5])<<8 | uint32(acl[i+6])<<16 | uint32(acl[i+7])<<24
		perm := uint16(acl[i+2]) | uint16(acl[i+3])<<8
		if tag == aclGroup && int(id) == gid && perm&6 == 6 {
			return true
		}
	}
	return false
}
