package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	inventoryapi "github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi"
	"github.com/gorundebug/inventory_service_api/pkg/generated/proto/inventoryserviceapi/processorderitem"
	"github.com/gorundebug/order_service_api/pkg/generated/openapi/orderserviceapi/processorder"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type InventoryClients struct {
	connections []*grpc.ClientConn
	clients     []inventoryapi.InventoryServiceApiClient
	next        atomic.Uint64
}

func NewInventoryClients(address string, count int, extraOptions ...grpc.DialOption) (*InventoryClients, error) {
	if count <= 0 {
		return nil, errors.New("inventory connection count must be positive")
	}
	pool := &InventoryClients{
		connections: make([]*grpc.ClientConn, 0, count),
		clients:     make([]inventoryapi.InventoryServiceApiClient, 0, count),
	}
	dialOptions := append(
		[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
		extraOptions...,
	)
	for range count {
		connection, err := grpc.NewClient(address, dialOptions...)
		if err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("create inventory gRPC client: %w", err)
		}
		pool.connections = append(pool.connections, connection)
		pool.clients = append(pool.clients, inventoryapi.NewInventoryServiceApiClient(connection))
	}
	return pool, nil
}

func (p *InventoryClients) Client() inventoryapi.InventoryServiceApiClient {
	index := p.next.Add(1) - 1
	return p.clients[index%uint64(len(p.clients))]
}

func (p *InventoryClients) Close() error {
	var result error
	for _, connection := range p.connections {
		result = errors.Join(result, connection.Close())
	}
	return result
}

type inventoryClient interface {
	Client() inventoryapi.InventoryServiceApiClient
}

type Service struct {
	inventory  inventoryClient
	timeout    time.Duration
	softMargin time.Duration
}

func NewService(inventory inventoryClient, timeout, softMargin time.Duration) *Service {
	return &Service{inventory: inventory, timeout: timeout, softMargin: softMargin}
}

type orderItem struct {
	orderID  string
	itemID   string
	sku      string
	quantity int
	price    float64
}

type itemResult struct {
	itemID       string
	sku          string
	requestedQty int
	availableQty int
	reserved     bool
	status       string
	price        float64
	err          string
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/processorder", s.processOrder)
	return mux
}

func (s *Service) processOrder(w http.ResponseWriter, request *http.Request) {
	var input processorder.ProcessOrderRequest
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(input.Items) == 0 {
		http.Error(w, "items must not be empty", http.StatusBadRequest)
		return
	}
	for _, item := range input.Items {
		if item.Quantity <= 0 {
			http.Error(w, "all quantities must be positive", http.StatusBadRequest)
			return
		}
	}

	orderID := request.Header.Get("X-Request-ID")
	if orderID == "" {
		orderID = uuid.NewString()
	}
	items := make([]orderItem, 0, len(input.Items))
	var originalTotal float64
	for _, inputItem := range input.Items {
		price := 0.0
		if inputItem.UnitPrice != nil {
			price = *inputItem.UnitPrice
			originalTotal += float64(inputItem.Quantity) * price
		}
		items = append(items, orderItem{
			orderID: orderID, itemID: inputItem.ItemId, sku: inputItem.Sku,
			quantity: int(inputItem.Quantity), price: price,
		})
	}

	ctx, cancel := context.WithTimeout(request.Context(), s.timeout)
	defer cancel()
	results := make(chan itemResult, len(items))
	go s.processItems(ctx, items, results)

	softTimeout := s.timeout - s.softMargin
	if softTimeout < 0 {
		softTimeout = 0
	}
	softTimer := time.NewTimer(softTimeout)
	defer softTimer.Stop()

	collected := make([]itemResult, 0, len(items))
	for len(collected) < len(items) {
		select {
		case result := <-results:
			collected = append(collected, result)
		case <-softTimer.C:
			cancel()
			s.writeResponse(w, orderID, "TIMED_OUT", originalTotal, nil)
			return
		case <-request.Context().Done():
			return
		}
	}

	status := "CONFIRMED"
	var total float64
	for _, result := range collected {
		total += result.price * float64(result.requestedQty)
		if !result.reserved {
			status = "PARTIALLY_CONFIRMED"
		}
	}
	s.writeResponse(w, orderID, status, total, collected)
}

func (s *Service) processItems(ctx context.Context, items []orderItem, results chan<- itemResult) {
	for _, item := range items {
		result := s.processItem(ctx, item)
		select {
		case results <- result:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) processItem(ctx context.Context, item orderItem) itemResult {
	result := itemResult{
		itemID: item.itemID, sku: item.sku, requestedQty: item.quantity, price: item.price,
	}
	response, err := s.inventory.Client().ProcessOrderItem(ctx, &processorderitem.ProcessOrderItemRequest{
		OrderId: item.orderID, ItemId: item.itemID, Sku: item.sku, Quantity: int32(item.quantity),
	})
	if err != nil {
		result.status = "PROCESSING_ERROR"
		result.err = err.Error()
	} else {
		result.availableQty = int(response.AvailableQty)
		result.reserved = response.Reserved
		result.status = response.Status
	}
	return result
}

func (s *Service) writeResponse(w http.ResponseWriter, orderID, status string, total float64, results []itemResult) {
	processedAt := time.Now()
	response := processorder.ProcessOrderResponse{
		OrderId: &orderID, Status: &status, TotalAmount: &total, ProcessedAt: &processedAt,
	}
	if len(results) > 0 {
		confirmed := make([]processorder.ProcessOrderResponseItem, 0, len(results))
		for _, result := range results {
			available := int32(result.availableQty)
			itemID, sku, reserved, itemStatus := result.itemID, result.sku, result.reserved, result.status
			item := processorder.ProcessOrderResponseItem{
				ItemId: &itemID, Sku: &sku, AvailableQty: &available,
				Reserved: &reserved, Status: &itemStatus,
			}
			if result.err != "" {
				errorText := result.err
				item.Error = &errorText
			}
			confirmed = append(confirmed, item)
		}
		response.ConfirmedItems = &confirmed
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
