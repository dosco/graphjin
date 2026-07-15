package tests_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

func TestIsMissingContainerImageError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "docker missing image", err: errors.New("container create: Error response from daemon: No such image: example/db:latest"), want: true},
		{name: "wrapped missing image", err: fmt.Errorf("create container: %w", errors.New("NO SUCH IMAGE: example/db:latest")), want: true},
		{name: "pull failure", err: errors.New("pull image: registry unavailable")},
		{name: "startup failure", err: errors.New("wait strategy: deadline exceeded")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMissingContainerImageError(tt.err); got != tt.want {
				t.Fatalf("isMissingContainerImageError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestStartGenericContainerRetriesMissingImageOnceWithForcedPull(t *testing.T) {
	missing := errors.New("container create: Error response from daemon: No such image: example/db:latest")
	calls := 0
	start := func(_ context.Context, req testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		calls++
		switch calls {
		case 1:
			if req.AlwaysPullImage {
				t.Fatal("first attempt unexpectedly forced an image pull")
			}
			return nil, missing
		case 2:
			if !req.AlwaysPullImage {
				t.Fatal("retry did not force an image pull")
			}
			return nil, nil
		default:
			t.Fatalf("unexpected container start attempt %d", calls)
			return nil, nil
		}
	}

	if _, err := startGenericContainerWith(context.Background(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "example/db:latest"},
	}, start); err != nil {
		t.Fatalf("retry returned an error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("container start calls = %d, want 2", calls)
	}
}

func TestStartGenericContainerDoesNotRetryOtherFailures(t *testing.T) {
	wantErr := errors.New("wait strategy: deadline exceeded")
	calls := 0
	start := func(_ context.Context, _ testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		calls++
		return nil, wantErr
	}

	_, err := startGenericContainerWith(context.Background(), testcontainers.GenericContainerRequest{}, start)
	if !errors.Is(err, wantErr) {
		t.Fatalf("start error = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("container start calls = %d, want 1", calls)
	}
}
