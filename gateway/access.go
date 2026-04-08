package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/edge"
	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
	"github.com/openziti/zrok/v2/sdk/golang/sdk"
)

type zrokAccessOps interface {
	CreateAccess(root env_core.Root, request *sdk.AccessRequest) (*sdk.Access, error)
	DeleteAccess(root env_core.Root, access *sdk.Access) error
}

type zitiDialContext interface {
	DialWithOptions(serviceName string, options *ziti.DialOptions) (edge.Conn, error)
	Close()
}

type zitiContextFactory interface {
	Load(root env_core.Root) (zitiDialContext, error)
}

type sdkAccessOps struct{}

func (sdkAccessOps) CreateAccess(root env_core.Root, request *sdk.AccessRequest) (*sdk.Access, error) {
	return sdk.CreateAccess(root, request)
}

func (sdkAccessOps) DeleteAccess(root env_core.Root, access *sdk.Access) error {
	return sdk.DeleteAccess(root, access)
}

type defaultZitiContextFactory struct{}

func (defaultZitiContextFactory) Load(root env_core.Root) (zitiDialContext, error) {
	zif, err := root.ZitiIdentityNamed(root.EnvironmentIdentityName())
	if err != nil {
		return nil, fmt.Errorf("failed to get ziti identity path: %w", err)
	}

	zcfg, err := ziti.NewConfigFromFile(zif)
	if err != nil {
		return nil, fmt.Errorf("failed to load ziti identity: %w", err)
	}

	zctx, err := ziti.NewContext(zcfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create ziti context: %w", err)
	}

	return zctx, nil
}

var defaultAccessOps zrokAccessOps = sdkAccessOps{}
var defaultContextFactory zitiContextFactory = defaultZitiContextFactory{}

// Access wraps zrok access lifecycle and provides an HTTP client.
type Access struct {
	root       env_core.Root
	access     *sdk.Access
	shareToken string
	httpClient *http.Client
	transport  *http.Transport
	zitiCtx    zitiDialContext
	accessOps  zrokAccessOps
	closeOnce  sync.Once
	closeErr   error
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

	return newAccess(root, shareToken, defaultAccessOps, defaultContextFactory)
}

func newAccess(root env_core.Root, shareToken string, accessOps zrokAccessOps, ctxFactory zitiContextFactory) (*Access, error) {
	dl.Infof("creating zrok access for share '%s'", shareToken)

	acc, err := accessOps.CreateAccess(root, &sdk.AccessRequest{ShareToken: shareToken})
	if err != nil {
		return nil, fmt.Errorf("failed to create access: %w", err)
	}

	dl.Infof("zrok access created for share '%s'", shareToken)

	zitiCtx, err := ctxFactory.Load(root)
	if err != nil {
		dl.Errorf("failed to create ziti context for share '%s': %v", shareToken, err)
		if deleteErr := accessOps.DeleteAccess(root, acc); deleteErr != nil {
			dl.Errorf("failed to delete access after ziti context setup error: %v", deleteErr)
		}
		return nil, err
	}

	a := &Access{
		root:       root,
		access:     acc,
		shareToken: shareToken,
		zitiCtx:    zitiCtx,
		accessOps:  accessOps,
	}

	a.httpClient, a.transport = a.createHTTPClient()

	return a, nil
}

// createHTTPClient creates an http.Client that routes through the zrok overlay.
func (a *Access) createHTTPClient() (*http.Client, *http.Transport) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// ignore network and addr, dial the share token directly through zrok
			return a.zitiCtx.DialWithOptions(a.shareToken, &ziti.DialOptions{ConnectTimeout: 30 * time.Second})
		},
	}
	return &http.Client{Transport: transport}, transport
}

// HTTPClient returns the http.Client that routes through zrok.
func (a *Access) HTTPClient() *http.Client {
	return a.httpClient
}

// Close terminates the zrok access.
func (a *Access) Close() error {
	a.closeOnce.Do(func() {
		var firstErr error

		if a.transport != nil {
			a.transport.CloseIdleConnections()
			a.transport = nil
		}

		if a.zitiCtx != nil {
			a.zitiCtx.Close()
			a.zitiCtx = nil
		}

		if a.access != nil {
			dl.Infof("deleting zrok access for share '%s'", a.shareToken)

			if err := a.accessOps.DeleteAccess(a.root, a.access); err != nil {
				dl.Errorf("error deleting access: %v", err)
				firstErr = err
			} else {
				dl.Infof("zrok access deleted for share '%s'", a.shareToken)
			}
			a.access = nil
		}

		a.httpClient = nil
		a.closeErr = firstErr
	})

	return a.closeErr
}
