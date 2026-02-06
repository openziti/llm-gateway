package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
	"github.com/openziti/zrok/v2/sdk/golang/sdk"
)

// Access wraps zrok access lifecycle and provides an HTTP client.
type Access struct {
	root       env_core.Root
	access     *sdk.Access
	shareToken string
	httpClient *http.Client
}

// NewAccess creates a zrok access and HTTP client for the share token.
func NewAccess(shareToken string) (*Access, error) {
	root, err := environment.LoadRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to load zrok environment: %w", err)
	}

	if !root.IsEnabled() {
		return nil, fmt.Errorf("zrok environment is not enabled; run 'zrok enable' first")
	}

	dl.Infof("creating zrok access for share '%s'", shareToken)

	acc, err := sdk.CreateAccess(root, &sdk.AccessRequest{ShareToken: shareToken})
	if err != nil {
		return nil, fmt.Errorf("failed to create access: %w", err)
	}

	dl.Infof("zrok access created for share '%s'", shareToken)

	a := &Access{
		root:       root,
		access:     acc,
		shareToken: shareToken,
	}

	a.httpClient = a.createHTTPClient()

	return a, nil
}

// createHTTPClient creates an http.Client that routes through the zrok overlay.
func (a *Access) createHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// ignore network and addr, dial the share token directly through zrok
				return sdk.NewDialer(a.shareToken, a.root)
			},
		},
	}
}

// HTTPClient returns the http.Client that routes through zrok.
func (a *Access) HTTPClient() *http.Client {
	return a.httpClient
}

// Close terminates the zrok access.
func (a *Access) Close() error {
	if a.access == nil {
		return nil
	}

	dl.Infof("deleting zrok access for share '%s'", a.shareToken)

	if err := sdk.DeleteAccess(a.root, a.access); err != nil {
		dl.Errorf("error deleting access: %v", err)
		return err
	}

	dl.Infof("zrok access deleted for share '%s'", a.shareToken)
	return nil
}
