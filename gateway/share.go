package gateway

import (
	"fmt"
	"net"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/sdk-golang/ziti/edge"
	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
	"github.com/openziti/zrok/v2/sdk/golang/sdk"
)

// Share wraps a zrok share lifecycle.
type Share struct {
	root       env_core.Root
	share      *sdk.Share
	listener   edge.Listener
	token      string
	persistent bool // if true, share is persistent (don't delete on close)
}

// NewShare creates a zrok share with the specified mode.
// mode can be "public", "private", or empty (defaults to "private").
func NewShare(mode string) (*Share, error) {
	root, err := environment.LoadRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to load zrok environment: %w", err)
	}

	if !root.IsEnabled() {
		return nil, fmt.Errorf("zrok environment is not enabled; run 'zrok enable' first")
	}

	shareMode := sdk.PrivateShareMode
	if mode == "public" {
		shareMode = sdk.PublicShareMode
	}

	dl.Infof("creating zrok %s share", shareMode)

	shareReq := &sdk.ShareRequest{
		BackendMode:    sdk.ProxyBackendMode,
		ShareMode:      shareMode,
		PermissionMode: sdk.OpenPermissionMode,
	}

	shr, err := sdk.CreateShare(root, shareReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create share: %w", err)
	}

	dl.Infof("created zrok share '%s'", shr.Token)

	listener, err := sdk.NewListener(shr.Token, root)
	if err != nil {
		if deleteErr := sdk.DeleteShare(root, shr); deleteErr != nil {
			dl.Errorf("failed to delete share after listener failure: %v", deleteErr)
		}
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	dl.Infof("listener ready for share '%s'", shr.Token)

	return &Share{
		root:       root,
		share:      shr,
		listener:   listener,
		token:      shr.Token,
		persistent: false,
	}, nil
}

// NewShareFromToken creates a Share from an existing persistent share token.
// Persistent shares are private shares that persist across restarts.
// The share won't be deleted on Close since it's managed externally.
func NewShareFromToken(token string) (*Share, error) {
	root, err := environment.LoadRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to load zrok environment: %w", err)
	}

	if !root.IsEnabled() {
		return nil, fmt.Errorf("zrok environment is not enabled; run 'zrok enable' first")
	}

	dl.Infof("connecting to existing zrok share '%s'", token)

	listener, err := sdk.NewListener(token, root)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener for share '%s': %w", token, err)
	}

	dl.Infof("listener ready for share '%s'", token)

	return &Share{
		root:       root,
		share:      nil,
		listener:   listener,
		token:      token,
		persistent: true,
	}, nil
}

// Token returns the share token for client access.
func (s *Share) Token() string {
	return s.token
}

// Listener returns the net.Listener for serving HTTP.
func (s *Share) Listener() net.Listener {
	return s.listener
}

// Close terminates the share and cleans up resources.
// For persistent shares, only the listener is closed (the share is managed externally).
func (s *Share) Close() error {
	var lastErr error

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			dl.Errorf("error closing listener: %v", err)
			lastErr = err
		}
	}

	// delete the share only if we created it (not persistent)
	if !s.persistent && s.share != nil && s.root != nil {
		if err := sdk.DeleteShare(s.root, s.share); err != nil {
			dl.Errorf("error deleting share: %v", err)
			lastErr = err
		}
	}

	dl.Infof("share '%s' closed", s.token)
	return lastErr
}
