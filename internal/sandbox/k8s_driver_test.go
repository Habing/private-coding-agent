package sandbox_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

func ensureK8sTestUser(t *testing.T, pg *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	usvc := user.NewService(user.NewRepo(pg))
	email := fmt.Sprintf("k8s-%d-%s@example.com", time.Now().UnixNano(), uuid.NewString()[:8])
	u, err := usvc.Register(ctx, tenantID, email, "irrelevant-password-XX", "K8sTester")
	require.NoError(t, err)
	return u.ID
}

// newK8sTestEnv wires a K8sDriver against the dockertest PG instance (set up
// by TestMain in sessionrepo_test.go) + a fake K8s clientset + a real Redis
// at localhost:6379. The clientset is returned so each test can drive Pod
// status transitions via UpdateStatus + reaction inspections.
func newK8sTestEnv(t *testing.T) (*sandbox.K8sDriver, *fake.Clientset, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	cs := fake.NewSimpleClientset()

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = rdb.Close() })

	repo := sandbox.NewSessionRepo(pg)
	d, err := sandbox.NewK8sDriver(cs, nil, repo, rdb, sandbox.K8sDriverConfig{
		Namespace:       "pca-sb-test",
		PodReadyTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	uid := ensureK8sTestUser(t, pg, tn.ID)
	return d, cs, tn.ID, uid
}

// markPodRunning is a goroutine helper: after `after`, scans the namespace
// for any Pod whose name has the pca-sb- prefix and flips its phase to
// Running so the driver's waitForPodReady poll can succeed. Returns a chan
// closed once the flip is applied (or ctx is cancelled).
func markPodRunning(t *testing.T, cs *fake.Clientset, ns string, after time.Duration) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(after)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for i := 0; i < 50; i++ {
			pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
			if err == nil {
				for _, p := range pods.Items {
					if !strings.HasPrefix(p.Name, "pca-sb-") {
						continue
					}
					pod := p.DeepCopy()
					pod.Status.Phase = corev1.PodRunning
					_, _ = cs.CoreV1().Pods(ns).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return done
}

func TestK8sDriver_Create_PodSpec(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()

	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)

	sb, err := d.Create(ctx, sandbox.CreateOpts{
		TenantID:    tenantID,
		OwnerUserID: userID,
		Network:     sandbox.NetworkInternal,
	})
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusRunning, sb.Status)

	pods, err := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, pods.Items, 1)
	p := pods.Items[0]
	require.True(t, strings.HasPrefix(p.Name, "pca-sb-"), "name=%s", p.Name)
	require.Equal(t, tenantID.String(), p.Labels["pca.tenant_id"])
	require.Equal(t, sb.ID.String(), p.Labels["pca.sandbox_id"])
	require.Equal(t, userID.String(), p.Labels["pca.owner_user_id"])
	require.Equal(t, "internal", p.Labels["pca.network"])
	require.Equal(t, []string{"sleep", "infinity"}, p.Spec.Containers[0].Command)
	require.Equal(t, "/workspace", p.Spec.Containers[0].WorkingDir)
}

func TestK8sDriver_Create_SecurityContext(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)

	_, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: sandbox.NetworkInternal})
	require.NoError(t, err)

	pods, _ := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
	require.Len(t, pods.Items, 1)
	sc := pods.Items[0].Spec.Containers[0].SecurityContext
	require.NotNil(t, sc)
	require.NotNil(t, sc.RunAsUser)
	require.Equal(t, int64(10001), *sc.RunAsUser)
	require.NotNil(t, sc.ReadOnlyRootFilesystem)
	require.True(t, *sc.ReadOnlyRootFilesystem)
	require.NotNil(t, sc.AllowPrivilegeEscalation)
	require.False(t, *sc.AllowPrivilegeEscalation)
	require.NotNil(t, sc.Capabilities)
	require.Equal(t, []corev1.Capability{"ALL"}, sc.Capabilities.Drop)
	require.ElementsMatch(t,
		[]corev1.Capability{"CHOWN", "DAC_OVERRIDE", "SETUID", "SETGID", "FOWNER"},
		sc.Capabilities.Add)
}

func TestK8sDriver_Create_Seccomp_RuntimeDefault(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)

	_, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: sandbox.NetworkInternal})
	require.NoError(t, err)

	pods, _ := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
	sp := pods.Items[0].Spec.Containers[0].SecurityContext.SeccompProfile
	require.NotNil(t, sp)
	require.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, sp.Type)
	require.Nil(t, sp.LocalhostProfile)
}

func TestK8sDriver_Create_Seccomp_Localhost(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	cs := fake.NewSimpleClientset()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = rdb.Close() })
	repo := sandbox.NewSessionRepo(pg)
	d, err := sandbox.NewK8sDriver(cs, nil, repo, rdb, sandbox.K8sDriverConfig{
		Namespace:               "pca-sb-test",
		PodReadyTimeout:         5 * time.Second,
		SeccompLocalhostProfile: "pca/sandbox-seccomp.json",
	})
	require.NoError(t, err)

	tn, _ := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	uid := ensureK8sTestUser(t, pg, tn.ID)
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)

	_, err = d.Create(ctx, sandbox.CreateOpts{TenantID: tn.ID, OwnerUserID: uid, Network: sandbox.NetworkInternal})
	require.NoError(t, err)

	pods, _ := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
	sp := pods.Items[0].Spec.Containers[0].SecurityContext.SeccompProfile
	require.Equal(t, corev1.SeccompProfileTypeLocalhost, sp.Type)
	require.NotNil(t, sp.LocalhostProfile)
	require.Equal(t, "pca/sandbox-seccomp.json", *sp.LocalhostProfile)
}

func TestK8sDriver_Create_ResourcesGuaranteed(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)

	_, err := d.Create(ctx, sandbox.CreateOpts{
		TenantID:    tenantID,
		OwnerUserID: userID,
		Network:     sandbox.NetworkInternal,
		Resources:   sandbox.ResourceLimits{CPUs: 2.0, MemoryMB: 1024, PIDsLimit: 256},
	})
	require.NoError(t, err)

	pods, _ := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
	r := pods.Items[0].Spec.Containers[0].Resources
	require.Equal(t, r.Requests[corev1.ResourceCPU], r.Limits[corev1.ResourceCPU], "Guaranteed QoS")
	require.Equal(t, r.Requests[corev1.ResourceMemory], r.Limits[corev1.ResourceMemory])
	cpu := r.Limits[corev1.ResourceCPU]
	mem := r.Limits[corev1.ResourceMemory]
	// resource.Quantity simplifies "2000m" → "2"; the canonical form for an
	// integer CPU is the bare integer (see k8s.io/apimachinery/pkg/api/resource).
	require.Equal(t, "2", cpu.String())
	// 1024Mi simplifies to 1Gi in canonical form.
	require.Equal(t, "1Gi", mem.String())
}

func TestK8sDriver_Create_PodReadyTimeout(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	cs := fake.NewSimpleClientset()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = rdb.Close() })
	repo := sandbox.NewSessionRepo(pg)
	d, err := sandbox.NewK8sDriver(cs, nil, repo, rdb, sandbox.K8sDriverConfig{
		Namespace:       "pca-sb-test",
		PodReadyTimeout: 300 * time.Millisecond,
	})
	require.NoError(t, err)

	tn, _ := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	uid := ensureK8sTestUser(t, pg, tn.ID)

	// Never mark running — driver must give up and delete the Pod.
	_, err = d.Create(ctx, sandbox.CreateOpts{TenantID: tn.ID, OwnerUserID: uid, Network: sandbox.NetworkInternal})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not ready")

	// Pod was deleted on cleanup path.
	pods, _ := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
	require.Empty(t, pods.Items, "timed-out pod must be deleted")
}

func TestK8sDriver_Get_TenantIsolation(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: sandbox.NetworkInternal})
	require.NoError(t, err)

	// Wrong tenant → ErrSandboxNotFound (no enumeration).
	otherTenant := uuid.New()
	_, err = d.Get(ctx, otherTenant, sb.ID)
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)

	// Right tenant → found.
	got, err := d.Get(ctx, tenantID, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sb.ID, got.ID)
}

func TestK8sDriver_Destroy_Idempotent(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: sandbox.NetworkInternal})
	require.NoError(t, err)

	require.NoError(t, d.Destroy(ctx, tenantID, sb.ID))
	// Second destroy is a no-op.
	require.NoError(t, d.Destroy(ctx, tenantID, sb.ID))

	// Pod is gone in the cluster too.
	pods, _ := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
	require.Empty(t, pods.Items)
}

func TestK8sDriver_Destroy_PodDeleteCalled(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: sandbox.NetworkInternal})
	require.NoError(t, err)

	var deleted atomic.Int32
	cs.PrependReactor("delete", "pods", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		deleted.Add(1)
		return false, nil, nil // fall through to default
	})

	require.NoError(t, d.Destroy(ctx, tenantID, sb.ID))
	require.Equal(t, int32(1), deleted.Load(), "Pods.Delete must be called exactly once on first destroy")
}

func TestK8sDriver_Snapshot_ReturnsDisabled(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: sandbox.NetworkInternal})
	require.NoError(t, err)
	_ = cs

	_, err = d.Snapshot(ctx, tenantID, sb.ID)
	require.ErrorIs(t, err, sandbox.ErrSnapshotDisabled)
}

func TestK8sDriver_Snapshot_TenantScopeBeforeDisabled(t *testing.T) {
	d, cs, tenantID, userID := newK8sTestEnv(t)
	ctx := context.Background()
	markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: sandbox.NetworkInternal})
	require.NoError(t, err)
	_ = cs

	// Wrong tenant must hit the scope check (ErrSandboxNotFound), NOT leak
	// existence by short-circuiting to ErrSnapshotDisabled.
	_, err = d.Snapshot(ctx, uuid.New(), sb.ID)
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
	require.False(t, errors.Is(err, sandbox.ErrSnapshotDisabled))
}

func TestK8sDriver_NetworkModes_Labels(t *testing.T) {
	cases := []sandbox.NetworkMode{sandbox.NetworkInternal, sandbox.NetworkBridge, sandbox.NetworkNone}
	for _, mode := range cases {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			d, cs, tenantID, userID := newK8sTestEnv(t)
			ctx := context.Background()
			markPodRunning(t, cs, "pca-sb-test", 200*time.Millisecond)
			_, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tenantID, OwnerUserID: userID, Network: mode})
			require.NoError(t, err)
			pods, _ := cs.CoreV1().Pods("pca-sb-test").List(ctx, metav1.ListOptions{})
			require.Len(t, pods.Items, 1)
			require.Equal(t, string(mode), pods.Items[0].Labels["pca.network"])
			if mode == sandbox.NetworkNone {
				require.Equal(t, corev1.DNSNone, pods.Items[0].Spec.DNSPolicy)
			} else {
				require.Equal(t, corev1.DNSClusterFirst, pods.Items[0].Spec.DNSPolicy)
			}
		})
	}
}

// TestK8sDriver_ExecStream_Compiles is a non-runtime assertion: it only
// confirms that K8sDriver.Exec compiles against remotecommand.StreamOptions
// and our k8sExecer abstraction. Actual exec/file behavior is verified by
// 22d2 kind nightly (fake client cannot serve the SPDY exec subresource).
func TestK8sDriver_ExecStream_Compiles(t *testing.T) {
	// Just take a reference so the package's exec types are linked into
	// the test binary. No runtime assertions.
	var _ remotecommand.StreamOptions
}
