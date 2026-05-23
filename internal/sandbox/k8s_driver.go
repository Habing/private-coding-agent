package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/yourorg/private-coding-agent/internal/logx"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

// K8sDriverConfig configures a K8sDriver.
type K8sDriverConfig struct {
	// Namespace is the K8s namespace where sandbox Pods are created.
	// Defaults to "pca-sandboxes" if empty. The chart pre-creates it; the
	// driver does NOT attempt to create it at boot.
	Namespace string

	// ServiceAccount is the SA the sandbox Pod runs under. Empty (default)
	// means automountServiceAccountToken=false and no serviceAccountName is
	// set — the recommended posture. Only set when the workload genuinely
	// needs an in-cluster SA (e.g., for downstream API calls); doing so
	// re-exposes attack surface a sandbox is normally meant to drop.
	ServiceAccount string

	// SeccompLocalhostProfile is the relative path under kubelet's
	// /var/lib/kubelet/seccomp/ where the hardened profile is staged.
	// Empty (default) → securityContext uses RuntimeDefault. Non-empty →
	// Localhost type with that path. Localhost requires the profile to
	// already be on every node (DaemonSet sync, image bake, etc.).
	SeccompLocalhostProfile string

	// PodReadyTimeout caps how long Create blocks waiting for the Pod to
	// reach phase=Running. Default 60s covers cold image pulls on slow
	// registries; tune down for hot caches. Hitting the timeout causes
	// Create to delete the Pod and return an error (no orphans).
	PodReadyTimeout time.Duration
}

// K8sDriver implements Runtime against a Kubernetes cluster — one Pod per
// sandbox. Snapshot returns ErrSnapshotDisabled in this driver (see slice
// 22d-v2 for kaniko-based snapshot support).
type K8sDriver struct {
	cs      kubernetes.Interface
	restCfg *rest.Config
	repo    *SessionRepo
	redis   *redis.Client
	cfg     K8sDriverConfig
	snaps   *SnapshotRepo

	// nowFn / pollInterval / execerFactory are test seams. Real code uses the
	// defaults set in NewK8sDriver; tests inject deterministic / faster
	// implementations.
	nowFn        func() time.Time
	pollInterval time.Duration
	execerFactory k8sExecerFactory
}

// k8sExecerFactory abstracts the remotecommand SPDY executor so unit tests
// can stub exec behavior without needing a real API server. The real
// implementation is newSPDYExecer in k8s_driver_exec.go.
type k8sExecerFactory func(restCfg *rest.Config, ns, podName string, opts ExecOpts) (k8sExecer, error)

// NewK8sDriver wires a K8sDriver. cs is a connected clientset; restCfg is the
// rest.Config used for SPDY exec subresources (may be nil for unit tests
// that don't exercise exec/files). repo persists session metadata; rdb is
// used for distributed Destroy locks (same lock key shape as DockerDriver).
//
// The namespace is NOT auto-created; the deploy chart owns its lifecycle.
// Snapshot dependencies (SnapshotRepo) default to nil; if SetSnapshotRepo
// is called, Destroy will call DetachSession on it (the K8s driver itself
// does not support Snapshot — it returns ErrSnapshotDisabled).
func NewK8sDriver(cs kubernetes.Interface, restCfg *rest.Config, repo *SessionRepo, rdb *redis.Client, cfg K8sDriverConfig) (*K8sDriver, error) {
	if cs == nil {
		return nil, errors.New("k8s driver: clientset is nil")
	}
	if repo == nil {
		return nil, errors.New("k8s driver: session repo is nil")
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "pca-sandboxes"
	}
	if cfg.PodReadyTimeout == 0 {
		cfg.PodReadyTimeout = 60 * time.Second
	}
	return &K8sDriver{
		cs:           cs,
		restCfg:      restCfg,
		repo:         repo,
		redis:        rdb,
		cfg:          cfg,
		nowFn:        time.Now,
		pollInterval: 500 * time.Millisecond,
		execerFactory: newSPDYExecer,
	}, nil
}

// SetSnapshotRepo wires the SnapshotRepo so Destroy can null session_id on
// any snapshots that referenced this Pod's session. Mirrors DockerDriver
// behavior — even though K8sDriver.Snapshot itself is disabled, snapshots
// may still exist if the operator previously ran in Docker mode against the
// same DB. Pass nil to leave the hook off.
func (d *K8sDriver) SetSnapshotRepo(snaps *SnapshotRepo) {
	d.snaps = snaps
}

// podName returns the deterministic Pod name for a sandbox UUID. We use the
// first 12 hex chars (post-dash-strip) because K8s name length max is 63
// and DNS-1123 disallows non-lowercase / underscore. 12 chars + "pca-sb-"
// prefix = 19 chars, well within limits, and 12 hex chars give 48 bits of
// entropy — more than enough for per-tenant namespacing.
func podName(id uuid.UUID) string {
	hex := strings.ReplaceAll(id.String(), "-", "")
	if len(hex) > 12 {
		hex = hex[:12]
	}
	return "pca-sb-" + hex
}

// Create allocates a new sandbox Pod. Blocks up to cfg.PodReadyTimeout for
// the Pod to reach phase=Running; on timeout or hard failure (ImagePullBackOff,
// CreateContainerError, PodFailed) the Pod is deleted and the sandbox row is
// transitioned to status=failed.
func (d *K8sDriver) Create(ctx context.Context, opts CreateOpts) (sandboxOut *Sandbox, createErr error) {
	ctx, span := tracer.Start(ctx, "sandbox.create.k8s",
		trace.WithAttributes(attribute.String("sandbox.image", opts.Image)))
	defer func() {
		if createErr != nil {
			span.RecordError(createErr)
			span.SetStatus(codes.Error, createErr.Error())
		} else if sandboxOut != nil {
			span.SetAttributes(attribute.String("sandbox.id", sandboxOut.ID.String()))
		}
		span.End()
	}()

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

	name := podName(sb.ID)
	pod := d.buildPod(sb, opts, name)

	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := d.cs.CoreV1().Pods(d.cfg.Namespace).Create(createCtx, pod, metav1.CreateOptions{}); err != nil {
		_ = d.repo.UpdateStatus(ctx, sb.ID, StatusFailed)
		return nil, fmt.Errorf("create pod: %w", err)
	}

	if err := d.waitForPodReady(ctx, name); err != nil {
		// Use detached ctx for cleanup so a canceled parent ctx still removes
		// the Pod we just created (same reasoning as DockerDriver).
		_ = d.cs.CoreV1().Pods(d.cfg.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
		_ = d.repo.UpdateStatus(ctx, sb.ID, StatusFailed)
		return nil, fmt.Errorf("wait pod ready: %w", err)
	}

	if err := d.repo.SetContainerID(ctx, sb.ID, name); err != nil {
		_ = d.cs.CoreV1().Pods(d.cfg.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("set container id: %w", err)
	}
	sb.Status = StatusRunning
	if pcametrics.SandboxActive != nil {
		pcametrics.SandboxActive.Add(ctx, 1)
	}
	return sb, nil
}

// waitForPodReady polls the Pod until phase=Running, hard-fails on
// PodFailed/PodSucceeded or known unrecoverable waiting reasons, or returns
// a timeout error after d.cfg.PodReadyTimeout. Polling — not watch — keeps
// the dependency footprint smaller and matches how kubelet checkpoints
// state internally.
func (d *K8sDriver) waitForPodReady(ctx context.Context, name string) error {
	deadline := d.nowFn().Add(d.cfg.PodReadyTimeout)
	for {
		pod, err := d.cs.CoreV1().Pods(d.cfg.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			switch pod.Status.Phase {
			case corev1.PodRunning:
				return nil
			case corev1.PodFailed, corev1.PodSucceeded:
				return fmt.Errorf("pod terminal phase=%s reason=%q",
					pod.Status.Phase, pod.Status.Reason)
			}
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					r := cs.State.Waiting.Reason
					if r == "ImagePullBackOff" || r == "ErrImagePull" || r == "CreateContainerError" {
						return fmt.Errorf("container %s waiting: %s — %s",
							cs.Name, r, cs.State.Waiting.Message)
					}
				}
			}
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get pod %s: %w", name, err)
		}
		if d.nowFn().After(deadline) {
			return fmt.Errorf("pod %s not ready after %s", name, d.cfg.PodReadyTimeout)
		}
		select {
		case <-time.After(d.pollInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// buildPod renders the corev1.Pod spec. Hardening posture is intentionally
// the strictest profile K8s exposes:
//   - runAsNonRoot + runAsUser/Group=10001 + readOnlyRootFilesystem
//   - capabilities drop ALL, add only the five fs-ownership caps the
//     sandbox bootstrap needs (mirrors DockerDriver CapAdd)
//   - allowPrivilegeEscalation=false (== no-new-privileges:true)
//   - seccompProfile Localhost (if cfg set) or RuntimeDefault
//   - automountServiceAccountToken=false (unless ServiceAccount is set)
//   - restartPolicy=Never — Pod ends when its container dies; we rebuild
//     by destroy+create rather than letting the kubelet restart in place
//     (which would silently lose tmpfs state).
//
// Resources are set as identical requests==limits → Guaranteed QoS, so
// noisy-neighbor sandboxes can't starve real workloads sharing the node.
func (d *K8sDriver) buildPod(sb *Sandbox, opts CreateOpts, name string) *corev1.Pod {
	runAsUser := int64(10001)
	runAsGroup := int64(10001)
	runAsNonRoot := true
	readOnlyRoot := true
	allowEsc := false

	caps := &corev1.Capabilities{
		Drop: []corev1.Capability{"ALL"},
		Add:  []corev1.Capability{"CHOWN", "DAC_OVERRIDE", "SETUID", "SETGID", "FOWNER"},
	}

	seccomp := &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	if d.cfg.SeccompLocalhostProfile != "" {
		profilePath := d.cfg.SeccompLocalhostProfile
		seccomp = &corev1.SeccompProfile{
			Type:             corev1.SeccompProfileTypeLocalhost,
			LocalhostProfile: &profilePath,
		}
	}

	cpuQty := resource.MustParse(fmt.Sprintf("%dm", int(opts.Resources.CPUs*1000)))
	memQty := resource.MustParse(fmt.Sprintf("%dMi", opts.Resources.MemoryMB))

	envList := make([]corev1.EnvVar, 0, len(opts.Env))
	for k, v := range opts.Env {
		envList = append(envList, corev1.EnvVar{Name: k, Value: v})
	}

	workspaceSize := resource.MustParse("1Gi")
	tmpSize := resource.MustParse("1Gi")

	automount := false
	saName := ""
	if d.cfg.ServiceAccount != "" {
		automount = true
		saName = d.cfg.ServiceAccount
	}

	dnsPolicy := corev1.DNSClusterFirst
	if opts.Network == NetworkNone {
		dnsPolicy = corev1.DNSNone
	}

	labels := map[string]string{
		"pca.tenant_id":     opts.TenantID.String(),
		"pca.sandbox_id":    sb.ID.String(),
		"pca.owner_user_id": opts.OwnerUserID.String(),
		"pca.network":       string(opts.Network),
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: d.cfg.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: &automount,
			ServiceAccountName:           saName,
			DNSPolicy:                    dnsPolicy,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:      &runAsUser,
				RunAsGroup:     &runAsGroup,
				RunAsNonRoot:   &runAsNonRoot,
				SeccompProfile: seccomp,
			},
			Volumes: []corev1.Volume{
				{Name: "workspace", VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium:    corev1.StorageMediumMemory,
						SizeLimit: &workspaceSize,
					},
				}},
				{Name: "tmp", VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium:    corev1.StorageMediumMemory,
						SizeLimit: &tmpSize,
					},
				}},
			},
			Containers: []corev1.Container{{
				Name:       "sandbox",
				Image:      opts.Image,
				Command:    []string{"sleep", "infinity"},
				WorkingDir: workspaceRoot,
				Env:        envList,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    cpuQty,
						corev1.ResourceMemory: memQty,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    cpuQty,
						corev1.ResourceMemory: memQty,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "workspace", MountPath: workspaceRoot},
					{Name: "tmp", MountPath: "/tmp"},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:                &runAsUser,
					RunAsGroup:               &runAsGroup,
					RunAsNonRoot:             &runAsNonRoot,
					ReadOnlyRootFilesystem:   &readOnlyRoot,
					AllowPrivilegeEscalation: &allowEsc,
					Capabilities:             caps,
					SeccompProfile:           seccomp,
				},
			}},
		},
	}
}

// Get returns the sandbox scoped to tenant.
func (d *K8sDriver) Get(ctx context.Context, tenantID, id uuid.UUID) (*Sandbox, error) {
	return d.repo.Get(ctx, tenantID, id)
}

// Destroy stops and removes the sandbox Pod. Idempotent. Mirrors
// DockerDriver.Destroy step-for-step: redis lock + lua release + status
// flip + detached cleanup ctx + DetachSession hook. The container_id
// column (reused) stores the Pod name.
func (d *K8sDriver) Destroy(ctx context.Context, tenantID, id uuid.UUID) (destroyErr error) {
	ctx, span := tracer.Start(ctx, "sandbox.destroy.k8s",
		trace.WithAttributes(attribute.String("sandbox.id", id.String())))
	defer func() {
		if destroyErr != nil {
			span.RecordError(destroyErr)
			span.SetStatus(codes.Error, destroyErr.Error())
		}
		span.End()
	}()

	lockKey := "pca:sandbox:destroy:" + id.String()
	lockVal := uuid.NewString()

	ok, err := d.redis.SetNX(ctx, lockKey, lockVal, destroyLockTTL).Result()
	if err != nil {
		return fmt.Errorf("acquire destroy lock: %w", err)
	}
	if !ok {
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
	defer func() {
		_, err := d.redis.Eval(context.Background(), destroyLockReleaseScript,
			[]string{lockKey}, lockVal).Result()
		if err != nil && err != redis.Nil {
			logx.FromCtx(ctx).Error("sandbox destroy: release lock",
				"lock_key", lockKey, "err", err.Error())
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

	name, err := d.repo.GetContainerID(ctx, sb.TenantID, sb.ID)
	if err != nil {
		logx.FromCtx(ctx).Error("sandbox destroy: get container_id",
			"sandbox_id", sb.ID.String(), "err", err.Error())
	}
	if name == "" {
		// Pending sandbox never got a Pod; nothing to delete in K8s. Fall
		// through to status flip + detach so the row reaches a terminal state.
		name = ""
	}
	if name != "" {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		grace := int64(destroyStopGracePeriod)
		err := d.cs.CoreV1().Pods(d.cfg.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{
			GracePeriodSeconds: &grace,
		})
		if err != nil && !apierrors.IsNotFound(err) {
			logx.FromCtx(ctx).Error("sandbox destroy: Pods.Delete",
				"pod", name, "err", err.Error())
		}
	}

	if err := d.repo.UpdateStatus(ctx, sb.ID, StatusDestroyed); err != nil {
		return err
	}
	if d.snaps != nil {
		if err := d.snaps.DetachSession(ctx, sb.TenantID, sb.ID); err != nil {
			logx.FromCtx(ctx).Warn("sandbox destroy: detach snapshots",
				"sandbox_id", sb.ID.String(), "err", err.Error())
		}
	}
	if pcametrics.SandboxActive != nil {
		pcametrics.SandboxActive.Add(ctx, -1)
	}
	return nil
}

// Snapshot is a tenant-scoped 503 in K8sDriver — the snapshot subsystem
// requires container-commit + image-save semantics that K8s does not
// expose. Returns ErrSandboxNotFound (NOT ErrSnapshotDisabled) when the
// id belongs to a different tenant, preserving the no-enumeration
// contract. ErrSnapshotDisabled is returned only when tenant scope checks
// out and we genuinely cannot snapshot.
//
// Slice 22d-v2 plans kaniko-based snapshot: kaniko builds a new image
// from the running Pod's filesystem and pushes it to the registry; until
// then this is unimplemented.
func (d *K8sDriver) Snapshot(ctx context.Context, tenantID, id uuid.UUID) (*Snapshot, error) {
	if _, err := d.repo.Get(ctx, tenantID, id); err != nil {
		return nil, err
	}
	return nil, ErrSnapshotDisabled
}

func (d *K8sDriver) RestoreFromSnapshot(ctx context.Context, tenantID, userID, snapshotID uuid.UUID) (*Sandbox, error) {
	if d.snaps != nil {
		if _, err := d.snaps.Get(ctx, tenantID, snapshotID); err != nil {
			return nil, err
		}
	}
	return nil, ErrSnapshotDisabled
}

// compile-time check that K8sDriver satisfies the Runtime interface.
var _ Runtime = (*K8sDriver)(nil)
