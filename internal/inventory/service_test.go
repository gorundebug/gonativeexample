package inventory

import (
	"context"
	"testing"

	"github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi/processorderitem"
)

func TestProcessOrderItem(t *testing.T) {
	service := NewService(0)

	confirmed, err := service.ProcessOrderItem(context.Background(), &processorderitem.ProcessOrderItemRequest{
		Sku: "SKU-001", Quantity: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed.Reserved || confirmed.Status != "CONFIRMED" || confirmed.AvailableQty != 3 {
		t.Fatalf("unexpected confirmed response: %+v", confirmed)
	}

	missing, err := service.ProcessOrderItem(context.Background(), &processorderitem.ProcessOrderItemRequest{
		Sku: "MISSING", Quantity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Reserved || missing.Status != "OUT_OF_STOCK" || missing.AvailableQty != 0 {
		t.Fatalf("unexpected missing response: %+v", missing)
	}
}
