package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
)

const credentialAcquisitionTimeout = 60 * time.Second
const credentialAcquisitionAttempts = 32

// Only an explicit persistence-gate rejection proves that the request did not
// reach the broker. Transport errors and provider responses are not replayable.
func isPersistenceRejection(err error) bool {
	var response *githubBrokerHTTPError
	return errors.As(err, &response) && response.status == http.StatusServiceUnavailable && response.code == "persistence_unavailable"
}

func (c *githubCredentialBrokerClient) resolveWithPersistenceRetry(ctx context.Context, attempts *int) (*githubBrokerCredential, error) {
	for *attempts < credentialAcquisitionAttempts {
		if err := ctx.Err(); err != nil {
			return nil, credentialAcquisitionError(*attempts, err)
		}
		(*attempts)++
		credential, err := c.resolve(ctx)
		if !isPersistenceRejection(err) {
			return credential, err
		}
		if *attempts == credentialAcquisitionAttempts {
			break
		}
		delayCap := time.Second << min(*attempts-1, 2)
		delay := delayCap/2 + time.Duration(rand.Int64N(int64(delayCap/2)))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, credentialAcquisitionError(*attempts, ctx.Err())
		case <-timer.C:
		}
	}
	return nil, credentialAcquisitionError(*attempts, errors.New("attempt limit reached"))
}

func credentialAcquisitionError(attempts int, cause error) error {
	return fmt.Errorf("resolve GitHub credential: persistence_unavailable after %d attempts: %w", attempts, cause)
}
