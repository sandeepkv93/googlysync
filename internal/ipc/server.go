package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/sandeepkv93/googlysync/internal/config"
	ipcgen "github.com/sandeepkv93/googlysync/internal/ipc/gen"
	"github.com/sandeepkv93/googlysync/internal/status"
)

// Server wraps the gRPC server for daemon IPC.
type Server struct {
	ipcgen.UnimplementedDaemonControlServer
	ipcgen.UnimplementedSyncStatusServer
	ipcgen.UnimplementedAuthServiceServer

	cfg    *config.Config
	logger *zap.Logger
	ver    string
	status *status.Store

	grpcServer *grpc.Server
	listener   net.Listener
}

// NewServer constructs a gRPC IPC server.
func NewServer(cfg *config.Config, logger *zap.Logger, statusStore *status.Store) (*Server, error) {
	return &Server{
		cfg:    cfg,
		logger: logger,
		ver:    "dev",
		status: statusStore,
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
	statusSnapshot := s.status.Current()
	return &ipcgen.StatusResponse{Status: toProtoStatus(statusSnapshot), RequestId: "req-0"}, nil
}

// WatchStatus streams periodic status updates until the client disconnects.
func (s *Server) WatchStatus(_ *ipcgen.Empty, stream ipcgen.SyncStatus_WatchStatusServer) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		statusSnapshot := s.status.Current()
		if err := stream.Send(&ipcgen.StatusResponse{Status: toProtoStatus(statusSnapshot), RequestId: "req-0"}); err != nil {
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

func toProtoStatus(snapshot status.Snapshot) *ipcgen.Status {
	return &ipcgen.Status{
		State:        mapState(snapshot.State),
		Message:      snapshot.Message,
		UpdatedAt:    toProtoTimestamp(snapshot.UpdatedAt),
		RecentEvents: toProtoEvents(snapshot.RecentEvents),
	}
}

func mapState(state status.State) ipcgen.Status_SyncState {
	switch state {
	case status.StateIdle:
		return ipcgen.Status_IDLE
	case status.StateSyncing:
		return ipcgen.Status_SYNCING
	case status.StateError:
		return ipcgen.Status_ERROR
	case status.StatePaused:
		return ipcgen.Status_PAUSED
	default:
		return ipcgen.Status_SYNC_STATE_UNSPECIFIED
	}
}

func statusError(err error) error {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return grpcstatus.FromContextError(err).Err()
	}
	return err
}
