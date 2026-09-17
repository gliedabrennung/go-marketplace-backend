//go:build integration

package objectstore_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	catalogapp "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/storage"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/objectstore"
)

const identities = `{"identities":[{"name":"marketplace","credentials":[{"accessKey":"marketplace","secretKey":"marketplace-secret"}],"actions":["Admin","Read","Write","List","Tagging"]}]}`

var endpoint string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()
	if external := os.Getenv("TEST_S3_ENDPOINT"); external != "" {
		endpoint = external
		return m.Run()
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "chrislusf/seaweedfs:3.80",
			ExposedPorts: []string{"8333/tcp"},
			Cmd: []string{"server", "-dir=/data", "-s3", "-s3.port=8333",
				"-s3.config=/etc/seaweedfs/s3.json", "-master.volumeSizeLimitMB=64"},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(identities),
				ContainerFilePath: "/etc/seaweedfs/s3.json",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForListeningPort("8333/tcp").WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seaweedfs: start: %v\n", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			fmt.Fprintf(os.Stderr, "seaweedfs: terminate: %v\n", err)
		}
	}()

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seaweedfs: host: %v\n", err)
		return 1
	}
	port, err := ctr.MappedPort(ctx, "8333/tcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "seaweedfs: port: %v\n", err)
		return 1
	}
	endpoint = "http://" + host + ":" + port.Port()
	return m.Run()
}

func newClient(t *testing.T) *objectstore.Client {
	t.Helper()
	client := objectstore.New(objectstore.Config{
		Endpoint:       endpoint,
		PublicEndpoint: endpoint,
		Region:         "us-east-1",
		Bucket:         "marketplace",
		AccessKey:      "marketplace",
		SecretKey:      "marketplace-secret",
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, client.EnsureBucket(ctx))
	require.NoError(t, client.Ping(ctx))
	return client
}

func TestSeaweedPresignedUploadAndDownload(t *testing.T) {
	ctx := context.Background()
	client := newClient(t)
	key := "catalog/products/presigned/original"
	payload := bytes.Repeat([]byte("marketplace"), 512)

	target, err := client.PresignPut(ctx, key, "image/png", int64(len(payload)), 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, target.Method)
	assert.True(t, target.ExpiresAt.After(time.Now()))

	req, err := http.NewRequestWithContext(ctx, target.Method, target.URL, bytes.NewReader(payload))
	require.NoError(t, err)
	for name, value := range target.Headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	info, err := client.Head(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, int64(len(payload)), info.Size)

	prefix, err := client.ReadPrefix(ctx, key, 11)
	require.NoError(t, err)
	assert.Equal(t, "marketplace", string(prefix))

	url, err := client.PresignGet(ctx, key, time.Minute)
	require.NoError(t, err)
	download, err := http.Get(url)
	require.NoError(t, err)
	got, err := io.ReadAll(download.Body)
	require.NoError(t, err)
	require.NoError(t, download.Body.Close())
	require.Equal(t, http.StatusOK, download.StatusCode)
	assert.Equal(t, payload, got)

	anonymous, err := http.Get(endpoint + "/marketplace/" + key)
	require.NoError(t, err)
	require.NoError(t, anonymous.Body.Close())
	assert.Equal(t, http.StatusForbidden, anonymous.StatusCode)
}

func TestSeaweedStorageAdapter(t *testing.T) {
	ctx := context.Background()
	files := storage.New(newClient(t))
	key := "catalog/imports/adapter/offers.csv"

	_, err := files.Head(ctx, key)
	require.ErrorIs(t, err, catalogapp.ErrUploadMissing)
	_, err = files.ReadPrefix(ctx, key, 16)
	require.ErrorIs(t, err, catalogapp.ErrUploadMissing)
	_, err = files.Open(ctx, key)
	require.ErrorIs(t, err, catalogapp.ErrUploadMissing)

	require.NoError(t, files.Put(ctx, key, "text/csv", []byte("seller_sku,price\nSKU-1,100\n")))
	info, err := files.Head(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, int64(27), info.Size)

	body, err := files.Open(ctx, key)
	require.NoError(t, err)
	content, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	assert.Contains(t, string(content), "SKU-1")

	signed, err := files.SignDownload(ctx, key)
	require.NoError(t, err)
	assert.Contains(t, signed, "X-Amz-Signature")
}
