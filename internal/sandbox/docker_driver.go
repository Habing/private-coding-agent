package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// DockerDriverConfig configures a DockerDriver.
type DockerDriverConfig struct {
	// InternalNetworkName 是给 NetworkInternal 模式的共享 internal 网络名。
	// 不存在时 DockerDriver 会自动创建。
	InternalNetworkName string
}

// DockerDriver implements Runtime using the local Docker daemon.
type DockerDriver struct {
	cli   *client.Client
	repo  *SessionRepo
	redis *redis.Client
	cfg   DockerDriverConfig
}

// NewDockerDriver wires a DockerDriver. cli must be a connected docker client;
// repo persists session metadata; redis is used for distributed Destroy locks.
//
// The internal network (cfg.InternalNetworkName, default "pca-sandbox-internal")
// is created if missing; idempotent.
func NewDockerDriver(ctx context.Context, cli *client.Client, repo *SessionRepo, rdb *redis.Client, cfg DockerDriverConfig) (*DockerDriver, error) {
	if cfg.InternalNetworkName == "" {
		cfg.InternalNetworkName = "pca-sandbox-internal"
	}
	d := &DockerDriver{cli: cli, repo: repo, redis: rdb, cfg: cfg}
	if err := d.ensureInternalNetwork(ctx); err != nil {
		return nil, fmt.Errorf("ensure internal network: %w", err)
	}
	return d, nil
}

func (d *DockerDriver) ensureInternalNetwork(ctx context.Context) error {
	f := filters.NewArgs()
	f.Add("name", d.cfg.InternalNetworkName)
	nets, err := d.cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	for _, n := range nets {
		if n.Name == d.cfg.InternalNetworkName {
			return nil
		}
	}
	_, err = d.cli.NetworkCreate(ctx, d.cfg.InternalNetworkName, network.CreateOptions{
		Driver:     "bridge",
		Internal:   true,
		Attachable: false,
	})
	if err != nil {
		// TOCTOU defense: between List and Create, another process / replica
		// may have created the same network. Docker returns "already exists"
		// in that case; treat it as success.
		if errdefs.IsConflict(err) || strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("create network %q: %w", d.cfg.InternalNetworkName, err)
	}
	return nil
}

// Create starts a new container per opts and persists metadata.
func (d *DockerDriver) Create(ctx context.Context, opts CreateOpts) (*Sandbox, error) {
	opts, err := NormalizeCreateOpts(opts)
	if err != nil {
		return nil, err
	}

	sb := &Sandbox{
		ID:          uuid.New(),
		TenantID:    opts.TenantID,
		OwnerUserID: opts.OwnerUserID,
		ProjectID:   opts.ProjectID,
		Image:       opts.Image,
		Status:      StatusPending,
		Network:     opts.Network,
		Resources:   opts.Resources,
	}
	if err := d.repo.Insert(ctx, sb); err != nil {
		return nil, err
	}

	cid, err := d.createAndStartContainer(ctx, sb, opts)
	if err != nil {
		_ = d.repo.UpdateStatus(ctx, sb.ID, StatusFailed)
		return nil, fmt.Errorf("create container: %w", err)
	}

	if err := d.repo.SetContainerID(ctx, sb.ID, cid); err != nil {
		// Use detached ctx for cleanup: parent ctx may be canceled by the
		// time we get here, which would silently skip the ContainerRemove
		// and leak a running container.
		_ = d.cli.ContainerRemove(context.Background(), cid, container.RemoveOptions{Force: true, RemoveVolumes: true})
		return nil, fmt.Errorf("set container id: %w", err)
	}
	sb.Status = StatusRunning
	return sb, nil
}

func (d *DockerDriver) createAndStartContainer(ctx context.Context, sb *Sandbox, opts CreateOpts) (string, error) {
	pidsLimit := opts.Resources.PIDsLimit
	cfg := &container.Config{
		Image:      opts.Image,
		Cmd:        []string{"sleep", "infinity"},
		WorkingDir: workspaceRoot,
		Labels: map[string]string{
			"pca.tenant_id":     opts.TenantID.String(),
			"pca.sandbox_id":    sb.ID.String(),
			"pca.owner_user_id": opts.OwnerUserID.String(),
		},
		Env: envToSlice(opts.Env),
	}
	hostCfg := &container.HostConfig{
		ReadonlyRootfs: true,
		Tmpfs: map[string]string{
			workspaceRoot: "size=1g,uid=10001,gid=10001",
			"/tmp":        "size=1g",
		},
		CapDrop:     strslice.StrSlice{"ALL"},
		CapAdd:      strslice.StrSlice{"CHOWN", "DAC_OVERRIDE", "SETUID", "SETGID", "FOWNER"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources: container.Resources{
			NanoCPUs:  int64(opts.Resources.CPUs * 1e9),
			Memory:    opts.Resources.MemoryMB * 1024 * 1024,
			PidsLimit: &pidsLimit,
		},
	}
	hostCfg.NetworkMode = networkModeFor(opts.Network, d.cfg.InternalNetworkName)

	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := d.cli.ContainerCreate(createCtx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", err
	}
	if err := d.cli.ContainerStart(createCtx, resp.ID, container.StartOptions{}); err != nil {
		_ = d.cli.ContainerRemove(context.Background(), resp.ID,
			container.RemoveOptions{Force: true, RemoveVolumes: true})
		return "", err
	}
	return resp.ID, nil
}

func envToSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func networkModeFor(mode NetworkMode, internalName string) container.NetworkMode {
	switch mode {
	case NetworkInternal:
		return container.NetworkMode(internalName)
	case NetworkBridge:
		return container.NetworkMode("bridge")
	case NetworkNone:
		return container.NetworkMode("none")
	}
	return container.NetworkMode("none")
}

// GetContainerIDForTest exposes container_id for integration tests. Not in the
// public Runtime interface.
func (d *DockerDriver) GetContainerIDForTest(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	return d.repo.GetContainerID(ctx, tenantID, id)
}
