//go:build integration

package testsupport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ContainerImage is the server the tests start when TEST_DATABASE_URL is unset: the major version
// production runs.
const ContainerImage = "postgres:17-alpine"

// startContainer runs a throwaway postgres:17 and returns its URL and a function that removes it.
// The password is random per run; the container is reachable only from this machine.
func startContainer(ctx context.Context) (dsn string, stop func(), err error) {
	// Older testcontainers releases panic when no Docker host can be found.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("docker is not available: %v", r)
		}
	}()
	password := strings.ReplaceAll(uuid.NewString(), "-", "")
	ctr, err := tcpostgres.Run(ctx, ContainerImage,
		tcpostgres.WithDatabase("ticketing_test"),
		tcpostgres.WithUsername("ticketing"),
		tcpostgres.WithPassword(password),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		if ctr != nil {
			_ = testcontainers.TerminateContainer(ctr)
		}
		return "", nil, err
	}
	dsn, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return "", nil, err
	}
	return dsn, func() { _ = testcontainers.TerminateContainer(ctr) }, nil
}
