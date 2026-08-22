package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorundebug/gonativeexample/internal/app"
	"github.com/gorundebug/gonativeexample/internal/inventory"
	benchmarkmiddleware "github.com/gorundebug/gonativeexample/internal/middleware"
	statushandler "github.com/gorundebug/gonativeexample/internal/status"
	inventoryapi "github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	delay, err := app.EnvDuration("INVENTORY_SERVICE_RESPONSE_DELAY", 0)
	if err != nil {
		return err
	}
	httpAddress := net.JoinHostPort(app.Env("INVENTORY_SERVICE_HTTP_HOST", "0.0.0.0"), app.Env("INVENTORY_SERVICE_HTTP_PORT", "9092"))
	grpcAddress := net.JoinHostPort(app.Env("INVENTORY_SERVICE_GRPC_HOST", "0.0.0.0"), app.Env("INVENTORY_SERVICE_GRPC_PORT", "9202"))

	listener, err := net.Listen("tcp", grpcAddress)
	if err != nil {
		return fmt.Errorf("listen gRPC: %w", err)
	}
	grpcServer := grpc.NewServer(grpc.StatsHandler(benchmarkmiddleware.GRPCStats{}))
	inventoryapi.RegisterInventoryServiceApiServer(grpcServer, inventory.NewService(delay))
	httpServer := &http.Server{Addr: httpAddress, Handler: benchmarkmiddleware.HTTP(statushandler.Handler("inventoryservice", time.Now())), ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- grpcServer.Serve(listener) }()
	go func() { errorsCh <- httpServer.ListenAndServe() }()

	select {
	case <-ctx.Done():
	case err = <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	stopped := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		grpcServer.Stop()
	}
	return nil
}
