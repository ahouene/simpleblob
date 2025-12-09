// Package s3testcontainer defines a testcontainer exposing an S3 API with Garage.
package s3testcontainer

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// An GarageContainer is a testcontainer that provides a S3-compatible API.
type GarageContainer struct {
	testcontainers.Container

	accessKey string
	secretKey string
}

const (
	// defaultImage is the tag:label for the Docker image used by default.
	defaultImage = "dxflrs/garage:v2.1.0"

	// region is the default region for Garage config.
	region = "garage"

	// apiPort is the port used for the S3 API.
	apiPort = "3900"

	// configTOML contains as little parameters as possible for the server to run.
	configTOML = `
metadata_dir = "/tmp/meta"
data_dir = "/tmp/data"
replication_factor = 1
rpc_bind_addr = "[::]:3901"
rpc_secret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

[s3_api]
s3_region = "` + region + `"
api_bind_addr = "[::]:` + apiPort + `"
`
)

// We are reading Garage identifiers through the CLI.
var (
	reNodeID    = regexp.MustCompile(`\b[0-9a-f]{16}\b`)
	reAccessKey = regexp.MustCompile(`\bGK[0-9a-f]{24}\b`)
	reSecretKey = regexp.MustCompile(`\b[0-9a-f]{64}\b`)
)

// Run starts a container running a S3-compatible (Garage) API, with the default image.
func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (container *GarageContainer, err error) {
	return RunContainer(ctx, defaultImage, opts...)
}

// RunContainer starts a container running a S3-compatible (Garage) API, with the given image.
func RunContainer(ctx context.Context, image string, opts ...testcontainers.ContainerCustomizer) (c *GarageContainer, err error) {
	c = new(GarageContainer)
	allOpts := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts("3900"),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(configTOML),
			ContainerFilePath: "/etc/garage.toml",
		}),
		testcontainers.WithWaitStrategy(wait.ForLog("listening")),
	}
	allOpts = append(allOpts, opts...)
	c.Container, err = testcontainers.Run(ctx, image, allOpts...)
	if err != nil {
		return nil, err
	}

	// Setup server following https://garagehq.deuxfleurs.fr/documentation/quick-start/

	// Get node identifier
	out, err := c.ExecGarage(ctx, "status")
	if err != nil {
		return nil, fmt.Errorf("getting node id: %w", err)
	}
	nodeID := reNodeID.FindString(out)

	// Create layout
	_, err = c.ExecGarage(ctx, "layout", "assign", "-z", "dc1", "-c", "128MiB", nodeID)
	if err != nil {
		return nil, fmt.Errorf("assigning layout: %w", err)
	}
	_, err = c.ExecGarage(ctx, "layout", "apply", "--version", "1")
	if err != nil {
		return nil, fmt.Errorf("applying layout: %w", err)
	}

	// Create credentials
	out, err = c.ExecGarage(ctx, "key", "create")
	if err != nil {
		return nil, fmt.Errorf("creating key: %w", err)
	}
	c.accessKey, c.secretKey = reAccessKey.FindString(out), reSecretKey.FindString(out)

	// Allow bucket creation
	_, err = c.ExecGarage(ctx, "key", "allow", "--create-bucket", c.accessKey)
	if err != nil {
		return nil, fmt.Errorf("allowing bucket creation: %w", err)
	}

	return c, nil
}

// ExecGarage is a convenience wrapper around (testcontainers.Container).Exec
// that runs a command with Garage CLI.
// It returns the combined outputs of the command.
func (c *GarageContainer) ExecGarage(ctx context.Context, cmdArgs ...string) (string, error) {
	cmd := append([]string{"/garage"}, cmdArgs...)
	exitCode, out, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", err
	}
	sb := new(strings.Builder)
	if _, err := io.Copy(sb, out); err != nil {
		return "", err
	}
	if exitCode != 0 {
		return "", fmt.Errorf("exit failure (code %d): %s", exitCode, sb.String())
	}
	return sb.String(), nil
}

// S3Endpoint returns the string to use as the S3 endpoint
// (what would be the AWS_ENDPOINT_URL environment variable).
func (c *GarageContainer) S3Endpoint(ctx context.Context) (string, error) {
	return c.PortEndpoint(ctx, "3900", "http")
}

// Region returns the S3 region for the container
// (what would be the AWS_DEFAULT_REGION environment variable).
func (*GarageContainer) Region() string { return region }

// AccessKey returns the Access Key ID for the container
// (what would be the AWS_ACCESS_KEY_ID environment variable).
func (c *GarageContainer) AccessKey() string { return c.accessKey }

// SecretKey returns the Key ID for the container
// (what would be the AWS_SECRET_ACCESS_KEY environment variable).
func (c *GarageContainer) SecretKey() string { return c.secretKey }
