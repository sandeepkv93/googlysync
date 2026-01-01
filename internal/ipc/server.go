package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/sandeepkv93/googlysync/internal/config"
	ipcgen "github.com/sandeepkv93/googlysync/internal/ipc/gen"
)

// Server wraps the gRPC server for daemon IPC.
type Server struct {
	ipcgen.UnimplementedDaemonControlServer
	ipcgen.UnimplementedSyncStatusServer
	ipcgen.UnimplementedAuthServiceServer

	cfg    *config.Config
	logger *zap.Logger
	ver    string

	grpcServer *grpc.Server
	listener   net.Listener

	statusMu sync.Mutex
	status   *ipcgen.Status
}

// NewServer constructs a gRPC IPC server.
func NewServer(cfg *config.Config, logger *zap.Logger) (*Server, error) {
	return &Server{
		cfg:    cfg,
		logger: logger,
		ver:    "dev",
		status: &ipcgen.Status{
			State:     ipcgen.Status_IDLE,
			Message:   "idle",
			UpdatedAt: timestamppb.New(time.Now()),
		},
	}, nil
}

// WithVersion sets the server version string.
func (s *Server) WithVersion(version string) {
	if version != "" {
		s.ver = version
	}
}

// Start begins serving over a Unix domain socket and blocks until ctx is done.
func (s *Server) Start(ctx context.Context) error {
	if s.cfg.SocketPath == "" {
		return errors.New("socket path not configured")
	}

	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o700); err != nil {
		return err
	}
	_ = os.Remove(s.cfg.SocketPath)

	ln, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return err
	}
	s.listener = ln

	s.grpcServer = grpc.NewServer()
	ipcgen.RegisterDaemonControlServer(s.grpcServer, s)
	ipcgen.RegisterSyncStatusServer(s.grpcServer, s)
	ipcgen.RegisterAuthServiceServer(s.grpcServer, s)

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("ipc server listening", zap.String("socket", s.cfg.SocketPath))
		errCh <- s.grpcServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		s.grpcServer.GracefulStop()
		_ = ln.Close()
		return nil
	case err := <-errCh:
		return err
	}
}

// Stop forces the gRPC server to stop.
func (s *Server) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

// Ping returns daemon version.
func (s *Server) Ping(ctx context.Context, _ *ipcgen.Empty) (*ipcgen.PingResponse, error) {
	_ = ctx
	return &ipcgen.PingResponse{Version: s.ver}, nil
}

// Shutdown is a placeholder for future graceful shutdown.
func (s *Server) Shutdown(ctx context.Context, _ *ipcgen.ShutdownRequest) (*ipcgen.ShutdownResponse, error) {
	_ = ctx
	return &ipcgen.ShutdownResponse{RequestId: "req-0"}, nil
}

// GetStatus returns a basic status snapshot.
func (s *Server) GetStatus(ctx context.Context, _ *ipcgen.Empty) (*ipcgen.StatusResponse, error) {
	_ = ctx
	status := s.snapshotStatus()
	return &ipcgen.StatusResponse{Status: status, RequestId: "req-0"}, nil
}

// WatchStatus streams periodic status updates until the client disconnects.
func (s *Server) WatchStatus(_ *ipcgen.Empty, stream ipcgen.SyncStatus_WatchStatusServer) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		status := s.snapshotStatus()
		if err := stream.Send(&ipcgen.StatusResponse{Status: status, RequestId: "req-0"}); err != nil {
			return err
		}
		select {
		case <-stream.Context().Done():
			return statusError(stream.Context().Err())
		case <-ticker.C:
		}
	}
}

// GetAuthState returns a stub auth state.
func (s *Server) GetAuthState(ctx context.Context, _ *ipcgen.Empty) (*ipcgen.AuthStateResponse, error) {
	_ = ctx
	return &ipcgen.AuthStateResponse{SignedIn: false, RequestId: "req-0"}, nil
}

func (s *Server) snapshotStatus() *ipcgen.Status {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	return &ipcgen.Status{
		State:     s.status.State,
		Message:   s.status.Message,
		UpdatedAt: timestamppb.New(time.Now()),
	}
}

func statusError(err error) error {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return status.FromContextError(err).Err()
	}
	return err
}
