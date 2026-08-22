package inventory

import (
	"context"
	"sync/atomic"
	"time"

	inventoryapi "github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi"
	"github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi/processorderitem"
)

// Service implements the generated inventory gRPC API without ServiceLib.
type Service struct {
	inventoryapi.UnimplementedInventoryServiceApiServer

	stock map[string]*atomic.Int64
	delay time.Duration
}

func NewService(delay time.Duration) *Service {
	return &Service{
		stock: map[string]*atomic.Int64{
			"SKU-001": atomicInt64(100),
			"SKU-002": atomicInt64(50),
			"SKU-003": atomicInt64(25),
		},
		delay: delay,
	}
}

func (s *Service) ProcessOrderItem(ctx context.Context, req *processorderitem.ProcessOrderItemRequest) (*processorderitem.ProcessOrderItemResponse, error) {
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	stock, ok := s.stock[req.Sku]
	if ok {
		quantity := int64(req.Quantity)
		for available := stock.Load(); available >= quantity; available = stock.Load() {
			if stock.CompareAndSwap(available, available-quantity) {
				return &processorderitem.ProcessOrderItemResponse{
					AvailableQty: req.Quantity,
					Reserved:     true,
					Status:       "CONFIRMED",
				}, nil
			}
		}
	}
	available := int64(0)
	if ok {
		available = stock.Load()
	}
	return &processorderitem.ProcessOrderItemResponse{
		AvailableQty: int32(available),
		Reserved:     false,
		Status:       "OUT_OF_STOCK",
	}, nil
}

func atomicInt64(value int64) *atomic.Int64 {
	result := &atomic.Int64{}
	result.Store(value)
	return result
}
