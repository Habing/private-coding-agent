package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log"
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

// Get returns the sandbox scoped to tenant.
func (d *DockerDriver) Get(ctx context.Context, tenantID, id uuid.UUID) (*Sandbox, error) {
	return d.repo.Get(ctx, tenantID, id)
}

const (
	destroyLockTTL         = 30 * time.Second
	destroyStopGracePeriod = 5 // seconds
)

// destroyLockReleaseScript 使用 Lua 原子地"按值比较再删除"。
// 防止 A 持锁 → 锁超时 → B 拿到锁 → A 苏醒误删 B 的锁。
const destroyLockReleaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0`

// Destroy stops and removes the sandbox. Idempotent.
func (d *DockerDriver) Destroy(ctx context.Context, tenantID, id uuid.UUID) error {
	lockKey := "pca:sandbox:destroy:" + id.String()
	lockVal := uuid.NewString()

	ok, err := d.redis.SetNX(ctx, lockKey, lockVal, destroyLockTTL).Result()
	if err != nil {
		return fmt.Errorf("acquire destroy lock: %w", err)
	}
	if !ok {
		// 锁被他人持有,等待最多 2 秒后重试一次,期间响应 ctx 取消
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		ok, err = d.redis.SetNX(ctx, lockKey, lockVal, destroyLockTTL).Result()
		if err != nil {
			return fmt.Errorf("retry destroy lock: %w", err)
		}
		if !ok {
			return fmt.Errorf("destroy already in progress")
		}
	}
	// 释放锁: 用 Lua 脚本按 value 匹配再 Del,防止误删他人锁
	defer func() {
		_, err := d.redis.Eval(context.Background(), destroyLockReleaseScript,
			[]string{lockKey}, lockVal).Result()
		if err != nil && err != redis.Nil {
			log.Printf("sandbox destroy: release lock %s: %v", lockKey, err)
		}
	}()

	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrSandboxNotFound) {
			return ErrSandboxNotFound
		}
		return err
	}
	if sb.Status == StatusDestroyed {
		return nil
	}

	if err := d.repo.UpdateStatus(ctx, sb.ID, StatusDestroying); err != nil {
		return err
	}

	cid, err := d.repo.GetContainerID(ctx, sb.TenantID, sb.ID)
	if err != nil {
		log.Printf("sandbox destroy: get container_id for %s: %v", sb.ID, err)
	}
	if cid != "" {
		// 容器层清理用 detached ctx + 短超时:即使调用方取消 ctx,
		// 我们也要把容器停掉,避免悬挂运行容器。
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		stopTimeout := destroyStopGracePeriod
		if err := d.cli.ContainerStop(cleanupCtx, cid, container.StopOptions{Timeout: &stopTimeout}); err != nil {
			log.Printf("sandbox destroy: ContainerStop %s: %v", cid, err)
		}
		if err := d.cli.ContainerRemove(cleanupCtx, cid, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			log.Printf("sandbox destroy: ContainerRemove %s: %v", cid, err)
		}
	}

	return d.repo.UpdateStatus(ctx, sb.ID, StatusDestroyed)
}

// Snapshot is reserved for future MinIO-backed workspace persistence.
// Returns ErrNotImplemented in Slice 2.
func (d *DockerDriver) Snapshot(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	// 仍然校验沙箱存在,防泄露
	if _, err := d.repo.Get(ctx, tenantID, id); err != nil {
		return "", err
	}
	return "", ErrNotImplemented
}
