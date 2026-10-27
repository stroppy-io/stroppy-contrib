package workload

import (
	"context"
	"fmt"
)

type tpccLoader interface {
	loadItem(ctx context.Context) error
	loadWarehouse(ctx context.Context, warehouse int) error
	loadStock(ctx context.Context, warehouse int) error
	loadDistrict(ctx context.Context, warehouse int) error
	loadCustomer(ctx context.Context, warehouse int, district int) error
	loadHistory(ctx context.Context, warehouse int, district int) error
	loadOrder(ctx context.Context, warehouse int, district int) ([]int, error)
	loadNewOrder(ctx context.Context, warehouse int, district int) error
	loadOrderLine(ctx context.Context, warehouse int, district int, olCnts []int) error
}

func prepareWorkload(ctx context.Context, w tpccLoader, threads, warehouses, threadID int) error {

	if threadID == 0 {
		if err := w.loadItem(ctx); err != nil {
			return fmt.Errorf("load item faield %v", err)
		}
	}

	for i := threadID % threads; i < warehouses; i += threads {
		warehouse := i%warehouses + 1
		if err := w.loadWarehouse(ctx, warehouse); err != nil {
			return fmt.Errorf("load warehouse in %d failed %v", warehouse, err)
		}
		if err := w.loadStock(ctx, warehouse); err != nil {
			return fmt.Errorf("load stock at warehouse %d failed %v", warehouse, err)
		}
		if err := w.loadDistrict(ctx, warehouse); err != nil {
			return fmt.Errorf("load district at wareshouse %d failed %v", warehouse, err)
		}
	}

	districts := warehouses * districtPerWarehouse
	var err error
	for i := threadID % threads; i < districts; i += threads {
		warehouse := (i/districtPerWarehouse)%warehouses + 1
		district := i%districtPerWarehouse + 1
		if err = w.loadCustomer(ctx, warehouse, district); err != nil {
			return fmt.Errorf("load customer at warehouse %d district %d failed %v", warehouse, district, err)
		}
		if err = w.loadHistory(ctx, warehouse, district); err != nil {
			return fmt.Errorf("load history at warehouse %d district %d failed %v", warehouse, district, err)
		}
		var olCnts []int
		if olCnts, err = w.loadOrder(ctx, warehouse, district); err != nil {
			return fmt.Errorf("load orders at warehouse %d district %d failed %v", warehouse, district, err)
		}
		if err = w.loadNewOrder(ctx, warehouse, district); err != nil {
			return fmt.Errorf("load new_order at warehouse %d district %d failed %v", warehouse, district, err)
		}
		if err = w.loadOrderLine(ctx, warehouse, district, olCnts); err != nil {
			return fmt.Errorf("load order_line at warehouse %d district %d failed %v", warehouse, district, err)
		}
	}

	return nil
}
