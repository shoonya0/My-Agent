package grpcserver

import (
	"errors"
	"fmt"
	"net"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Registrar registers gRPC service implementations on the server.
type Registrar func(srv *grpc.Server)

// Start creates a gRPC server, calls register to attach services, registers
// reflection for grpcurl/grpcui, listens on ":"+port, and runs Serve in a
// background goroutine. The caller should stop the returned server when
// shutting down (e.g. defer srv.GracefulStop()); this package does not handle
// signals.
func Start(port string, register Registrar, log *zap.Logger, opts ...grpc.ServerOption) (*grpc.Server, error) {
	addr := ":" + port
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("grpcserver: listen on port %s: %w", port, err)
	}

	srv := grpc.NewServer(opts...)
	register(srv)
	reflection.Register(srv)

	go func() {
		log.Info("gRPC listening", zap.String("addr", addr))
		if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Error("gRPC serve error", zap.Error(err))
		}
	}()

	return srv, nil
}
