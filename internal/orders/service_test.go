package orders

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	inventoryapi "github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi"
	"github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi/processorderitem"
	"github.com/gorundebug/order_service_api/pkg/generated/openapi/orderserviceapi/processorder"
	"google.golang.org/grpc"
)

type fakePool struct {
	client inventoryapi.InventoryServiceApiClient
}

func (p fakePool) Client() inventoryapi.InventoryServiceApiClient { return p.client }

type fakeClient struct {
	delay    time.Duration
	response *processorderitem.ProcessOrderItemResponse
	err      error
}

type trackingClient struct {
	active    atomic.Int32
	maxActive atomic.Int32
}

func (c *trackingClient) ProcessOrderItem(ctx context.Context, _ *processorderitem.ProcessOrderItemRequest, _ ...grpc.CallOption) (*processorderitem.ProcessOrderItemResponse, error) {
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for {
		maximum := c.maxActive.Load()
		if active <= maximum || c.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return &processorderitem.ProcessOrderItemResponse{Reserved: true, Status: "CONFIRMED"}, nil
	}
}

func (c fakeClient) ProcessOrderItem(ctx context.Context, _ *processorderitem.ProcessOrderItemRequest, _ ...grpc.CallOption) (*processorderitem.ProcessOrderItemResponse, error) {
	if c.delay > 0 {
		timer := time.NewTimer(c.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return c.response, c.err
}

func TestProcessOrderOutOfStock(t *testing.T) {
	response := invoke(t, fakeClient{response: &processorderitem.ProcessOrderItemResponse{Status: "OUT_OF_STOCK"}}, time.Second, 100*time.Millisecond)
	if response.Status == nil || *response.Status != "PARTIALLY_CONFIRMED" {
		t.Fatalf("unexpected order status: %+v", response.Status)
	}
	if response.ConfirmedItems == nil || len(*response.ConfirmedItems) != 1 {
		t.Fatalf("unexpected confirmed items: %+v", response.ConfirmedItems)
	}
	item := (*response.ConfirmedItems)[0]
	if item.Status == nil || *item.Status != "OUT_OF_STOCK" || item.Reserved == nil || *item.Reserved {
		t.Fatalf("unexpected item result: %+v", item)
	}
}

func TestProcessOrderPublishesProcessingError(t *testing.T) {
	response := invoke(t, fakeClient{err: errors.New("inventory unavailable")}, time.Second, 100*time.Millisecond)
	item := (*response.ConfirmedItems)[0]
	if item.Status == nil || *item.Status != "PROCESSING_ERROR" {
		t.Fatalf("unexpected item status: %+v", item.Status)
	}
	if item.Error == nil || !strings.Contains(*item.Error, "inventory unavailable") {
		t.Fatalf("unexpected item error: %+v", item.Error)
	}
}

func TestProcessOrderSoftDeadlineCancelsDelayedCall(t *testing.T) {
	started := time.Now()
	response := invoke(t, fakeClient{delay: 10 * time.Second}, 120*time.Millisecond, 20*time.Millisecond)
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("soft deadline took too long: %s", elapsed)
	}
	if response.Status == nil || *response.Status != "TIMED_OUT" {
		t.Fatalf("unexpected status: %+v", response.Status)
	}
	if response.ConfirmedItems != nil {
		t.Fatalf("timed out response must not contain results: %+v", response.ConfirmedItems)
	}
}

func TestProcessOrderProcessesItemsSequentially(t *testing.T) {
	client := &trackingClient{}
	body := `{"items":[` +
		`{"item_id":"one","sku":"SKU-001","quantity":1},` +
		`{"item_id":"two","sku":"SKU-002","quantity":1},` +
		`{"item_id":"three","sku":"SKU-003","quantity":1}` +
		`]}`
	response := invokeBody(t, client, time.Second, 100*time.Millisecond, body)
	if response.Status == nil || *response.Status != "CONFIRMED" {
		t.Fatalf("unexpected status: %+v", response.Status)
	}
	if maximum := client.maxActive.Load(); maximum != 1 {
		t.Fatalf("items were processed concurrently: max active calls = %d", maximum)
	}
}

func invoke(t *testing.T, client inventoryapi.InventoryServiceApiClient, timeout, margin time.Duration) processorder.ProcessOrderResponse {
	t.Helper()
	body := `{"customer_id":"customer","items":[{"item_id":"item","sku":"MISSING","quantity":1,"unit_price":799}]}`
	return invokeBody(t, client, timeout, margin, body)
}

func invokeBody(t *testing.T, client inventoryapi.InventoryServiceApiClient, timeout, margin time.Duration, body string) processorder.ProcessOrderResponse {
	t.Helper()
	service := NewService(fakePool{client: client}, timeout, margin)
	request := httptest.NewRequest(http.MethodPost, "/v1/processorder", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	service.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected HTTP status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response processorder.ProcessOrderResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}
