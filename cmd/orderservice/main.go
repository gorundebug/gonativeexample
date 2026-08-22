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
	benchmarkmiddleware "github.com/gorundebug/gonativeexample/internal/middleware"
	"github.com/gorundebug/gonativeexample/internal/orders"
	statushandler "github.com/gorundebug/gonativeexample/internal/status"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	connections, err := app.EnvInt("INVENTORY_SERVICE_API_CONNECTIONS_COUNT", 1)
	if err != nil {
		return err
	}
	timeout, err := app.EnvDuration("ORDER_SERVICE_REQUEST_TIMEOUT", 5*time.Second)
	if err != nil {
		return err
	}
	margin, err := app.EnvDuration("ORDER_SERVICE_SOFT_DEADLINE_MARGIN", time.Second)
	if err != nil {
		return err
	}
	if margin > timeout {
		return fmt.Errorf("ORDER_SERVICE_SOFT_DEADLINE_MARGIN must not exceed ORDER_SERVICE_REQUEST_TIMEOUT")
	}
	clients, err := orders.NewInventoryClients(app.Env("INVENTORY_SERVICE_API_ADDRESS", "inventoryservice:9202"), connections, grpc.WithStatsHandler(benchmarkmiddleware.GRPCStats{}))
	if err != nil {
		return err
	}
	defer clients.Close()

	service := orders.NewService(clients, timeout, margin)
	mux := http.NewServeMux()
	mux.Handle("/v1/processorder", service.Handler())
	mux.Handle("/status/data", statushandler.Handler("orderservice", time.Now()))
	mux.Handle("/metrics", statushandler.Handler("orderservice", time.Now()))
	address := net.JoinHostPort(app.Env("ORDER_SERVICE_HTTP_HOST", "0.0.0.0"), app.Env("ORDER_SERVICE_HTTP_PORT", "9091"))
	httpServer := &http.Server{Addr: address, Handler: benchmarkmiddleware.HTTP(mux), ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- httpServer.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
