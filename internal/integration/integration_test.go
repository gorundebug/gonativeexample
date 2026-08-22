package integration

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorundebug/gonativeexample/internal/inventory"
	"github.com/gorundebug/gonativeexample/internal/orders"
	inventoryapi "github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi"
	"github.com/gorundebug/order_service_api/pkg/generated/openapi/orderserviceapi/processorder"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

func TestOrderHTTPToInventoryGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	inventoryapi.RegisterInventoryServiceApiServer(grpcServer, inventory.NewService(0))
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(grpcServer.Stop)

	clients, err := orders.NewInventoryClients(
		"passthrough:///inventory",
		2,
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clients.Close() })
	orderService := orders.NewService(clients, 5*time.Second, time.Second)

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/processorder",
		strings.NewReader(`{"customer_id":"benchmark-customer","items":[{"item_id":"benchmark-item","sku":"BENCHMARK-MISSING-SKU","quantity":1,"unit_price":799}]}`),
	)
	responseRecorder := httptest.NewRecorder()
	orderService.Handler().ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	var response processorder.ProcessOrderResponse
	if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Status == nil || *response.Status != "PARTIALLY_CONFIRMED" {
		t.Fatalf("unexpected response status: %+v", response.Status)
	}
	if response.ConfirmedItems == nil || len(*response.ConfirmedItems) != 1 {
		t.Fatalf("unexpected item results: %+v", response.ConfirmedItems)
	}
	item := (*response.ConfirmedItems)[0]
	if item.Status == nil || *item.Status != "OUT_OF_STOCK" {
		t.Fatalf("unexpected inventory result: %+v", item)
	}
}
