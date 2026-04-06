package gateway

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/edge"
	"github.com/openziti/zrok/v2/environment/env_core"
	"github.com/openziti/zrok/v2/sdk/golang/sdk"
)

type fakeAccessOps struct {
	created     int
	deleted     int
	createErr   error
	deleteErr   error
	lastRequest *sdk.AccessRequest
	lastAccess  *sdk.Access
}

func (f *fakeAccessOps) CreateAccess(_ env_core.Root, request *sdk.AccessRequest) (*sdk.Access, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created++
	f.lastRequest = request
	f.lastAccess = &sdk.Access{Token: "frontend-token", ShareToken: request.ShareToken}
	return f.lastAccess, nil
}

func (f *fakeAccessOps) DeleteAccess(_ env_core.Root, access *sdk.Access) error {
	f.deleted++
	f.lastAccess = access
	return f.deleteErr
}

type fakeContextFactory struct {
	ctx     *fakeZitiContext
	loadErr error
	loads   int
}

func (f *fakeContextFactory) Load(_ env_core.Root) (zitiDialContext, error) {
	f.loads++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.ctx == nil {
		f.ctx = &fakeZitiContext{}
	}
	return f.ctx, nil
}

type fakeZitiContext struct {
	dials       int
	closed      int
	lastService string
	lastTimeout time.Duration
}

func (f *fakeZitiContext) DialWithOptions(serviceName string, options *ziti.DialOptions) (edge.Conn, error) {
	f.dials++
	f.lastService = serviceName
	if options != nil {
		f.lastTimeout = options.ConnectTimeout
	}
	server, client := net.Pipe()
	go func() {
		_ = server.Close()
	}()
	return &fakeEdgeConn{Conn: client}, nil
}

func (f *fakeZitiContext) Close() {
	f.closed++
}

type fakeEdgeConn struct {
	net.Conn
}

func (f *fakeEdgeConn) CloseWrite() error {
	return nil
}

func (f *fakeEdgeConn) IsClosed() bool {
	return false
}

func (f *fakeEdgeConn) GetAppData() []byte {
	return nil
}

func (f *fakeEdgeConn) SourceIdentifier() string {
	return ""
}

func (f *fakeEdgeConn) TraceRoute(hops uint32, timeout time.Duration) (*edge.TraceRouteResult, error) {
	return nil, nil
}

func (f *fakeEdgeConn) GetCircuitId() string {
	return ""
}

func (f *fakeEdgeConn) GetStickinessToken() []byte {
	return nil
}

func (f *fakeEdgeConn) Id() uint32 {
	return 1
}

func (f *fakeEdgeConn) GetRouterId() string {
	return ""
}

func (f *fakeEdgeConn) GetState() string {
	return ""
}

func (f *fakeEdgeConn) CompleteAcceptSuccess() error {
	return nil
}

func (f *fakeEdgeConn) CompleteAcceptFailed(err error) {
}

func TestNewAccessCreatesPersistentTransport(t *testing.T) {
	ops := &fakeAccessOps{}
	ctxFactory := &fakeContextFactory{ctx: &fakeZitiContext{}}

	access, err := newAccess(nil, "share-token", ops, ctxFactory)
	if err != nil {
		t.Fatalf("newAccess returned error: %v", err)
	}

	if ops.created != 1 {
		t.Fatalf("CreateAccess called %d times, want 1", ops.created)
	}
	if ctxFactory.loads != 1 {
		t.Fatalf("Load called %d times, want 1", ctxFactory.loads)
	}
	if access.HTTPClient() == nil {
		t.Fatal("expected HTTP client")
	}
	if access.transport == nil {
		t.Fatal("expected transport")
	}
	if access.HTTPClient().Transport != access.transport {
		t.Fatal("expected client to reuse stored transport")
	}
}

func TestAccessTransportReusesSingleZitiContext(t *testing.T) {
	ops := &fakeAccessOps{}
	ctx := &fakeZitiContext{}
	ctxFactory := &fakeContextFactory{ctx: ctx}

	access, err := newAccess(nil, "share-token", ops, ctxFactory)
	if err != nil {
		t.Fatalf("newAccess returned error: %v", err)
	}

	for i := 0; i < 2; i++ {
		conn, err := access.transport.DialContext(context.Background(), "tcp", "example.com:443")
		if err != nil {
			t.Fatalf("DialContext returned error: %v", err)
		}
		_ = conn.Close()
	}

	if ctxFactory.loads != 1 {
		t.Fatalf("Load called %d times, want 1", ctxFactory.loads)
	}
	if ctx.dials != 2 {
		t.Fatalf("DialWithOptions called %d times, want 2", ctx.dials)
	}
	if ctx.lastService != "share-token" {
		t.Fatalf("serviceName = %q, want share-token", ctx.lastService)
	}
	if ctx.lastTimeout != 30*time.Second {
		t.Fatalf("connect timeout = %s, want 30s", ctx.lastTimeout)
	}
}

func TestNewAccessDeletesAccessOnContextSetupFailure(t *testing.T) {
	ops := &fakeAccessOps{}
	ctxFactory := &fakeContextFactory{loadErr: errors.New("boom")}

	access, err := newAccess(nil, "share-token", ops, ctxFactory)
	if err == nil {
		t.Fatal("expected error")
	}
	if access != nil {
		t.Fatal("expected nil access on failure")
	}
	if ops.created != 1 {
		t.Fatalf("CreateAccess called %d times, want 1", ops.created)
	}
	if ops.deleted != 1 {
		t.Fatalf("DeleteAccess called %d times, want 1", ops.deleted)
	}
}

func TestAccessCloseClosesResourcesAndIsIdempotent(t *testing.T) {
	ops := &fakeAccessOps{}
	ctx := &fakeZitiContext{}
	ctxFactory := &fakeContextFactory{ctx: ctx}

	access, err := newAccess(nil, "share-token", ops, ctxFactory)
	if err != nil {
		t.Fatalf("newAccess returned error: %v", err)
	}

	if err := access.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := access.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}

	if ctx.closed != 1 {
		t.Fatalf("ziti context closed %d times, want 1", ctx.closed)
	}
	if ops.deleted != 1 {
		t.Fatalf("DeleteAccess called %d times, want 1", ops.deleted)
	}
	if access.transport != nil {
		t.Fatal("expected transport to be nil after close")
	}
	if access.access != nil {
		t.Fatal("expected access to be nil after close")
	}
	if access.httpClient != nil {
		t.Fatal("expected httpClient to be nil after close")
	}
}

func TestAccessCloseReturnsDeleteErrorAfterClosingLocalResources(t *testing.T) {
	ops := &fakeAccessOps{deleteErr: errors.New("delete failed")}
	ctx := &fakeZitiContext{}
	ctxFactory := &fakeContextFactory{ctx: ctx}

	access, err := newAccess(nil, "share-token", ops, ctxFactory)
	if err != nil {
		t.Fatalf("newAccess returned error: %v", err)
	}

	err = access.Close()
	if err == nil || err.Error() != "delete failed" {
		t.Fatalf("Close error = %v, want delete failed", err)
	}
	if ctx.closed != 1 {
		t.Fatalf("ziti context closed %d times, want 1", ctx.closed)
	}
	if access.transport != nil {
		t.Fatal("expected transport to be nil after close")
	}
	if access.httpClient != nil {
		t.Fatal("expected httpClient to be nil after close")
	}
}
