package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// RunReconciler scans active sandboxes against docker, marking dead containers
// as destroyed. Called once at server startup before serving traffic.
//
// Returns error only on infrastructure failure (DB unavailable); individual
// container inspect errors are logged and skipped.
func RunReconciler(ctx context.Context, repo *SessionRepo, cli *client.Client) error {
	active, err := repo.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active: %w", err)
	}
	if len(active) == 0 {
		return nil
	}

	log.Printf("reconciler: %d active sandbox(es) to verify", len(active))

	for _, sb := range active {
		cid, err := repo.GetContainerID(ctx, sb.TenantID, sb.ID)
		if err != nil {
			log.Printf("reconciler: get container_id %s: %v", sb.ID, err)
			continue
		}
		if cid == "" {
			// pending without container_id - mark destroyed
			_ = repo.UpdateStatus(ctx, sb.ID, StatusDestroyed)
			continue
		}
		_, err = cli.ContainerInspect(ctx, cid)
		if err != nil {
			if errdefs.IsNotFound(err) || isDockerNotFound(err) {
				_ = repo.UpdateStatus(ctx, sb.ID, StatusDestroyed)
				continue
			}
			log.Printf("reconciler: inspect %s: %v", cid, err)
			continue
		}
		// container exists: keep status
	}
	return nil
}

func isDockerNotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound interface{ NotFound() bool }
	if errors.As(err, &notFound) {
		return notFound.NotFound()
	}
	return false
}
