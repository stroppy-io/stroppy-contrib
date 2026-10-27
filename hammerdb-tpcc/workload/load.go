package workload

import (
	"context"
	"math"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	"github.com/stroppy-io/stroppy/v6/pkg/gen"
)

const itemsPerWarehouse = 100000
const customersPerDistrict = 3000

type warehouseRow struct {
	ID      int     `db:"w_id"`
	Name    string  `db:"w_name"`
	Street1 string  `db:"w_street_1"`
	Street2 string  `db:"w_street_2"`
	City    string  `db:"w_city"`
	State   string  `db:"w_state"`
	Zip     string  `db:"w_zip"`
	Tax     float64 `db:"w_tax"`
	YTD     float64 `db:"w_ytd"`
}

type districtRow struct {
	ID        int     `db:"d_id"`
	Warehouse int     `db:"d_w_id"`
	Name      string  `db:"d_name"`
	Street1   string  `db:"d_street_1"`
	Street2   string  `db:"d_street_2"`
	City      string  `db:"d_city"`
	State     string  `db:"d_state"`
	Zip       string  `db:"d_zip"`
	Tax       float64 `db:"d_tax"`
	YTD       float64 `db:"d_ytd"`
	NextOrder int     `db:"d_next_o_id"`
}

type itemRow struct {
	ID    int     `db:"i_id"`
	Image int     `db:"i_im_id"`
	Name  string  `db:"i_name"`
	Price float64 `db:"i_price"`
	Data  string  `db:"i_data"`
}

type stockRow struct {
	Item      int    `db:"s_i_id"`
	Warehouse int    `db:"s_w_id"`
	Quantity  int    `db:"s_quantity"`
	Dist01    string `db:"s_dist_01"`
	Dist02    string `db:"s_dist_02"`
	Dist03    string `db:"s_dist_03"`
	Dist04    string `db:"s_dist_04"`
	Dist05    string `db:"s_dist_05"`
	Dist06    string `db:"s_dist_06"`
	Dist07    string `db:"s_dist_07"`
	Dist08    string `db:"s_dist_08"`
	Dist09    string `db:"s_dist_09"`
	Dist10    string `db:"s_dist_10"`
	Data      string `db:"s_data"`
	YTD       int    `db:"s_ytd"`
	Orders    int    `db:"s_order_cnt"`
	Remote    int    `db:"s_remote_cnt"`
}

type customerRow struct {
	ID            int       `db:"c_id"`
	District      int       `db:"c_d_id"`
	Warehouse     int       `db:"c_w_id"`
	First         string    `db:"c_first"`
	Middle        string    `db:"c_middle"`
	Last          string    `db:"c_last"`
	Street1       string    `db:"c_street_1"`
	Street2       string    `db:"c_street_2"`
	City          string    `db:"c_city"`
	State         string    `db:"c_state"`
	Zip           string    `db:"c_zip"`
	Phone         string    `db:"c_phone"`
	Since         time.Time `db:"c_since"`
	Credit        string    `db:"c_credit"`
	CreditLimit   float64   `db:"c_credit_lim"`
	Discount      float64   `db:"c_discount"`
	Balance       float64   `db:"c_balance"`
	Data          string    `db:"c_data"`
	YTDPayment    float64   `db:"c_ytd_payment"`
	PaymentCount  int       `db:"c_payment_cnt"`
	DeliveryCount int       `db:"c_delivery_cnt"`
}

type historyRow struct {
	Customer          int       `db:"h_c_id"`
	CustomerDistrict  int       `db:"h_c_d_id"`
	CustomerWarehouse int       `db:"h_c_w_id"`
	Warehouse         int       `db:"h_w_id"`
	District          int       `db:"h_d_id"`
	Date              time.Time `db:"h_date"`
	Amount            float64   `db:"h_amount"`
	Data              string    `db:"h_data"`
}

type orderRow struct {
	ID        int       `db:"o_id"`
	Customer  int       `db:"o_c_id"`
	District  int       `db:"o_d_id"`
	Warehouse int       `db:"o_w_id"`
	Entry     time.Time `db:"o_entry_d"`
	Carrier   *int      `db:"o_carrier_id"`
	Lines     int       `db:"o_ol_cnt"`
	AllLocal  int       `db:"o_all_local"`
}

type newOrderRow struct {
	Order     int `db:"no_o_id"`
	District  int `db:"no_d_id"`
	Warehouse int `db:"no_w_id"`
}

type orderLineRow struct {
	Order           int        `db:"ol_o_id"`
	District        int        `db:"ol_d_id"`
	Warehouse       int        `db:"ol_w_id"`
	Number          int        `db:"ol_number"`
	Item            int        `db:"ol_i_id"`
	SupplyWarehouse int        `db:"ol_supply_w_id"`
	Quantity        int        `db:"ol_quantity"`
	Amount          float64    `db:"ol_amount"`
	Dist            string     `db:"ol_dist_info"`
	Delivery        *time.Time `db:"ol_delivery_d"`
}

func insertRows[T any](ctx context.Context, b *bench.Bench, table string, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	source := gen.FromRows(int64(len(rows)), func(index uint64) (T, error) { return rows[index], nil }, gen.BatchRows(1000), gen.MaxBytes(500))
	method := bench.InsertNative
	if b.DriverTypeName() == bench.DriverMySQL {
		method = bench.InsertPlainBulk
	}
	_, err := b.Insert(ctx, table, source, bench.LoadWorkers(1), bench.InsertMethod(method))
	return err
}

func itemRows(r *random, count int) []itemRow {
	originals := r.originals(count)
	rows := make([]itemRow, count)
	for i := range rows {
		row := &rows[i]
		row.ID, row.Image, row.Name = i+1, r.number(1, 10000), r.alpha(14, 24)
		row.Price = float64(r.number(100, 10000)) / 100
		row.Data = r.original(r.alpha(26, 50), originals[i+1])
	}
	return rows
}

func stockRows(r *random, warehouse, count int) []stockRow {
	originals := r.originals(count)
	rows := make([]stockRow, count)
	for i := range rows {
		row := &rows[i]
		row.Item, row.Warehouse, row.Quantity = i+1, warehouse, r.number(10, 100)
		row.Dist01, row.Dist02, row.Dist03 = r.alpha(24, 24), r.alpha(24, 24), r.alpha(24, 24)
		row.Dist04, row.Dist05, row.Dist06 = r.alpha(24, 24), r.alpha(24, 24), r.alpha(24, 24)
		row.Dist07, row.Dist08, row.Dist09, row.Dist10 = r.alpha(24, 24), r.alpha(24, 24), r.alpha(24, 24), r.alpha(24, 24)
		row.Data = r.original(r.alpha(26, 50), originals[i+1])
	}
	return rows
}

func customerRows(r *random, warehouse, district, first, count int, now time.Time) ([]customerRow, []historyRow) {
	customers := make([]customerRow, count)
	history := make([]historyRow, count)
	for i := range customers {
		row := &customers[i]
		row.ID, row.District, row.Warehouse = first+i, district, warehouse
		row.First, row.Middle = r.alpha(8, 16), "OE"
		if row.ID <= 1000 {
			row.Last = lastname(row.ID - 1)
		} else {
			row.Last = lastname(r.nurand())
		}
		address := r.address()
		row.Street1, row.Street2, row.City, row.State, row.Zip = address.Street1, address.Street2, address.City, address.State, address.Zip
		row.Phone, row.Since = r.phone(), now
		row.Credit = "BC"
		if r.number(0, 1) == 1 {
			row.Credit = "GC"
		}
		row.CreditLimit, row.Discount, row.Balance = 50000, float64(r.number(0, 50))/100, -10
		row.Data, row.YTDPayment, row.PaymentCount = r.alpha(300, 500), 10, 1
		history[i] = historyRow{row.ID, district, warehouse, warehouse, district, now, 10, r.alpha(12, 24)}
	}
	return customers, history
}

// HammerDB v6.1 assigns "set $cust($r) $t", leaving cust($r) unchanged.
// Preserve that asymmetric shuffle rather than substituting a permutation.
func customerOrder(r *random, count int) []int {
	customers := make([]int, count+1)
	for i := 1; i <= count; i++ {
		customers[i] = i
	}
	for i := 1; i <= count; i++ {
		customers[i] = customers[r.number(i, count)]
	}
	return customers
}

func orderRows(r *random, warehouse, district, first, count int, customers []int, now time.Time) ([]orderRow, []newOrderRow, []orderLineRow) {
	orders := make([]orderRow, 0, count)
	pending := make([]newOrderRow, 0, count)
	lines := make([]orderLineRow, 0, count*10)
	for id := first; id < first+count; id++ {
		carrier, lineCount := r.number(1, 10), r.number(5, 15)
		row := orderRow{id, customers[id], district, warehouse, now, &carrier, lineCount, 1}
		if id > 2100 {
			row.Carrier = nil
			pending = append(pending, newOrderRow{id, district, warehouse})
		}
		orders = append(orders, row)
		for number := 1; number <= lineCount; number++ {
			item := r.number(1, itemsPerWarehouse)
			line := orderLineRow{Order: id, District: district, Warehouse: warehouse, Number: number, Item: item, SupplyWarehouse: warehouse, Quantity: 5, Dist: r.alpha(24, 24)}
			if id <= 2100 {
				line.Amount, line.Delivery = float64(r.number(10, 10000))/100, &now
			}
			lines = append(lines, line)
		}
	}
	return orders, pending, lines
}

func (w *workload) loadItems(ctx context.Context, b *bench.Bench) error {
	r := newRandom(w.seed)
	if err := insertRows(ctx, b, "item", itemRows(r, itemsPerWarehouse)); err != nil {
		return err
	}
	w.itemRandom = r
	return nil
}

func loaderRange(workers, warehouses, index int) (int, int) {
	chunk, remainder := warehouses/workers, warehouses%workers
	position := index + 2
	first := chunk*index + 1
	if remainder > chunk {
		if position-1 <= remainder {
			chunk++
			first = chunk*index + 1
		} else {
			first += remainder
		}
	}
	last := first + chunk - 1
	if position == workers+1 {
		last = warehouses
	}
	return first, last
}

func (w *workload) load(ctx context.Context, b *bench.Bench) error {
	index := int(w.nextLoader.Add(1) - 1)
	workers := min(w.loadWorkers, w.warehouses)
	first, last := loaderRange(workers, w.warehouses, index)
	r := newRandom(w.seed + int64(index))
	if workers == 1 && w.itemRandom != nil {
		r = w.itemRandom
	}
	for warehouse := first; warehouse <= last; warehouse++ {
		name, address := r.alpha(6, 10), r.address()
		row := warehouseRow{warehouse, name, address.Street1, address.Street2, address.City, address.State, address.Zip, float64(r.number(10, 20)) / 100, 300000}
		if err := insertRows(ctx, b, "warehouse", []warehouseRow{row}); err != nil {
			return err
		}
		if err := insertRows(ctx, b, "stock", stockRows(r, warehouse, itemsPerWarehouse)); err != nil {
			return err
		}
		for district := 1; district <= 10; district++ {
			name, address := r.alpha(6, 10), r.address()
			row := districtRow{district, warehouse, name, address.Street1, address.Street2, address.City, address.State, address.Zip, float64(r.number(10, 20)) / 100, 30000, 3001}
			if err := insertRows(ctx, b, "district", []districtRow{row}); err != nil {
				return err
			}
		}
	}
	for warehouse := first; warehouse <= last; warehouse++ {
		for district := 1; district <= 10; district++ {
			for start := 1; start <= customersPerDistrict; start += 1000 {
				customers, history := customerRows(r, warehouse, district, start, 1000, time.Now().Truncate(time.Second))
				if err := insertRows(ctx, b, "customer", customers); err != nil {
					return err
				}
				if err := insertRows(ctx, b, "history", history); err != nil {
					return err
				}
			}
		}
	}
	for warehouse := first; warehouse <= last; warehouse++ {
		for district := 1; district <= 10; district++ {
			customers := customerOrder(r, customersPerDistrict)
			for start := 1; start <= customersPerDistrict; start += 100 {
				orders, pending, lines := orderRows(r, warehouse, district, start, 100, customers, time.Now().Truncate(time.Second))
				if w.mysql {
					for i := range lines {
						lines[i].Amount = math.Round(lines[i].Amount)
					}
				}
				if err := insertRows(ctx, b, "orders", orders); err != nil {
					return err
				}
				if err := insertRows(ctx, b, "new_order", pending); err != nil {
					return err
				}
				if err := insertRows(ctx, b, "order_line", lines); err != nil {
					return err
				}
			}
		}
	}
	b.Log.Info("HammerDB warehouses loaded", "first", first, "last", last)
	return nil
}
