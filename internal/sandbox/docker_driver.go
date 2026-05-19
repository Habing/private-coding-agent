package sandbox

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
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
		return err
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
	return err
}
