package objstore_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/objstore"
)

func TestNew_RequiresEndpoint(t *testing.T) {
	_, err := objstore.New(objstore.Config{Bucket: "b"})
	require.Error(t, err)
}

func TestNew_RequiresBucket(t *testing.T) {
	_, err := objstore.New(objstore.Config{Endpoint: "minio:9000"})
	require.Error(t, err)
}

func TestNew_OK(t *testing.T) {
	c, err := objstore.New(objstore.Config{
		Endpoint:  "minio:9000",
		Bucket:    "pca-snapshots",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
		UseSSL:    false,
	})
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Equal(t, "pca-snapshots", c.Bucket())
}
