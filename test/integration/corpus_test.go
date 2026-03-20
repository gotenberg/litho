//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// corpus describes a test corpus that can be provisioned via a Docker container.
type corpus struct {
	// Name identifies this corpus in log output (e.g. "pdfjs").
	Name string

	// ContextDir is the path (relative to the integration test directory) to
	// the Docker build context containing the Dockerfile and scripts.
	ContextDir string

	// OutputDir is the path (relative to the integration test directory) where
	// the container writes its output. Bind-mounted as /output in the container.
	OutputDir string

	// ReadyFunc returns true when the corpus is already provisioned and does
	// not need to be regenerated.
	ReadyFunc func() bool

	// Timeout is the maximum time allowed for the container to complete.
	Timeout time.Duration
}

// provision ensures this corpus exists on disk by running its container if
// needed. It streams container logs to stdout with a [name] prefix.
func (c *corpus) provision() error {
	forceRegen := os.Getenv("LITHO_REGEN_CORPUS") != ""

	if !forceRegen && c.ReadyFunc() {
		fmt.Printf("[%s] corpus already provisioned, skipping (set LITHO_REGEN_CORPUS=1 to force)\n", c.Name)
		return nil
	}

	absOutputDir, err := filepath.Abs(c.OutputDir)
	if err != nil {
		return fmt.Errorf("[%s] resolving output dir: %w", c.Name, err)
	}

	if err := os.MkdirAll(absOutputDir, 0o755); err != nil {
		return fmt.Errorf("[%s] creating output dir: %w", c.Name, err)
	}

	absContextDir, err := filepath.Abs(c.ContextDir)
	if err != nil {
		return fmt.Errorf("[%s] resolving docker context dir: %w", c.Name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	fmt.Printf("[%s] starting corpus generation container...\n", c.Name)

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:       absContextDir,
			Dockerfile:    "Dockerfile",
			KeepImage:     true,
			PrintBuildLog: true,
		},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Binds = []string{absOutputDir + ":/output"}
		},
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Consumers: []testcontainers.LogConsumer{
				&prefixedLogConsumer{prefix: c.Name},
			},
		},
		WaitingFor: wait.ForExit(),
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("[%s] starting container: %w", c.Name, err)
	}
	defer func() {
		_ = ctr.Terminate(context.Background())
	}()

	state, err := ctr.State(ctx)
	if err != nil {
		return fmt.Errorf("[%s] getting container state: %w", c.Name, err)
	}

	if state.ExitCode != 0 {
		logs, logErr := ctr.Logs(ctx)
		if logErr == nil {
			defer logs.Close()
			tail, _ := io.ReadAll(io.LimitReader(logs, 4096))
			return fmt.Errorf("[%s] generation failed (exit %d):\n%s", c.Name, state.ExitCode, string(tail))
		}
		return fmt.Errorf("[%s] generation failed with exit code %d", c.Name, state.ExitCode)
	}

	fmt.Printf("[%s] corpus generation complete\n", c.Name)
	return nil
}

// prefixedLogConsumer prints container log lines to stdout with a [name] prefix
// so the user can follow generation progress in real time.
type prefixedLogConsumer struct {
	prefix string
}

func (c *prefixedLogConsumer) Accept(log testcontainers.Log) {
	fmt.Printf("[%s] %s\n", c.prefix, strings.TrimRight(string(log.Content), "\n"))
}
