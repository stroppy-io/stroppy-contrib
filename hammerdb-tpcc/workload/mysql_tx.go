package workload

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

func mysqlQuote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "''") + "'"
}

func (w *workload) mysqlTransaction(ctx context.Context, s *bench.Tx, t *terminal, kind int, c mysqlCall) error {
	switch kind {
	case 0:
		return mysqlNewOrder(ctx, s, t, c)
	case 1:
		return mysqlPayment(ctx, s, t, c)
	case 2:
		return mysqlDelivery(ctx, s, t, c)
	case 3:
		if err := s.Exec(ctx, "start transaction", nil); err != nil {
			return err
		}
		next, err := s.QueryValue[int64](ctx, fmt.Sprintf("SELECT d_next_o_id FROM district WHERE d_w_id=%d AND d_id=%d", t.home, t.district), nil)
		if err != nil {
			return err
		}
		if err := s.Exec(ctx, fmt.Sprintf("SELECT COUNT(DISTINCT (s_i_id)) FROM order_line, stock WHERE ol_w_id = %d AND ol_d_id = %d AND (ol_o_id < %d) AND ol_o_id >= (%d - 20) AND s_w_id = %d AND s_i_id = ol_i_id AND s_quantity < %d", t.home, t.district, next, next, t.home, c.threshold), nil); err != nil {
			return err
		}
	default:
		return mysqlOrderStatus(ctx, s, t, c)
	}
	return s.Exec(ctx, "COMMIT", nil)
}

func mysqlNewOrder(ctx context.Context, s *bench.Tx, t *terminal, c mysqlCall) error {
	type customer struct {
		Discount float64 `db:"c_discount"`
		Last     string  `db:"c_last"`
		Credit   string  `db:"c_credit"`
		Tax      float64 `db:"w_tax"`
	}
	person, err := s.QueryValue[customer](ctx, fmt.Sprintf("SELECT c_discount, c_last, c_credit, w_tax FROM customer, warehouse WHERE warehouse.w_id = %d AND customer.c_w_id = %d AND customer.c_d_id = %d AND customer.c_id = %d", t.home, t.home, c.district, c.customer), nil)
	if err != nil {
		return err
	}
	if err := s.Exec(ctx, "start transaction", nil); err != nil {
		return err
	}
	type district struct {
		Next int     `db:"d_next_o_id"`
		Tax  float64 `db:"d_tax"`
	}
	d, err := s.QueryValue[district](ctx, fmt.Sprintf("SELECT d_next_o_id, d_tax FROM district WHERE d_id = %d AND d_w_id = %d FOR UPDATE", c.district, t.home), nil)
	if err != nil {
		return err
	}
	if err := s.Exec(ctx, fmt.Sprintf("UPDATE district SET d_next_o_id = d_next_o_id + 1 WHERE d_id = %d AND d_w_id = %d", c.district, t.home), nil); err != nil {
		return err
	}
	t.stream.Lock()
	rollback := t.random.number(1, 99)
	t.stream.Unlock()
	local := 1
	for line := 1; line <= c.lines; line++ {
		t.stream.Lock()
		item := 100001
		if line != c.lines || rollback != 1 {
			item = t.random.number(1, 100000)
		}
		supply := t.home
		if t.random.number(1, 100) <= 1 {
			local = 0
			for supply == t.home && t.warehouses != 1 {
				supply = t.random.number(1, t.warehouses)
			}
		}
		quantity := t.random.number(1, 10)
		t.stream.Unlock()
		type itemData struct {
			Price float64 `db:"i_price"`
			Name  string  `db:"i_name"`
			Data  string  `db:"i_data"`
		}
		items, err := s.QueryValues[itemData](ctx, fmt.Sprintf("SELECT i_price, i_name, i_data FROM item WHERE i_id = %d", item), nil)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return s.Exec(ctx, "ROLLBACK", nil)
		}
		row, err := s.RawRow(ctx, fmt.Sprintf("SELECT s_quantity, s_data, s_dist_01, s_dist_02, s_dist_03, s_dist_04, s_dist_05, s_dist_06, s_dist_07, s_dist_08, s_dist_09, s_dist_10 FROM stock WHERE s_i_id = %d AND s_w_id = %d", item, supply), nil)
		if err != nil {
			return err
		}
		if len(row) != 12 {
			return fmt.Errorf("stock %d/%d missing", supply, item)
		}
		stock, err := strconv.Atoi(fmt.Sprint(row[0]))
		if err != nil {
			return err
		}
		if stock > quantity {
			stock -= quantity
		} else {
			stock = stock - quantity + 91
		}
		if err := s.Exec(ctx, fmt.Sprintf("UPDATE stock SET s_quantity = %d WHERE s_i_id = %d AND s_w_id = %d", stock, item, supply), nil); err != nil {
			return err
		}
		amount := float64(quantity) * items[0].Price * (1 + person.Tax + d.Tax) * (1 - person.Discount)
		if err := s.Exec(ctx, fmt.Sprintf("INSERT INTO order_line (ol_o_id, ol_d_id, ol_w_id, ol_number, ol_i_id, ol_supply_w_id, ol_quantity, ol_amount, ol_dist_info) VALUES (%d, %d, %d, %d, %d, %d, %d, %.17g, %s)", d.Next, c.district, t.home, line, item, supply, quantity, amount, mysqlQuote(fmt.Sprint(row[c.district+1]))), nil); err != nil {
			return err
		}
	}
	if err := s.Exec(ctx, fmt.Sprintf("INSERT INTO orders (o_id, o_d_id, o_w_id, o_c_id, o_entry_d, o_ol_cnt, o_all_local) VALUES (%d, %d, %d, %d, %s, %d, %d)", d.Next, c.district, t.home, c.customer, c.date, c.lines, local), nil); err != nil {
		return err
	}
	if err := s.Exec(ctx, fmt.Sprintf("INSERT INTO new_order (no_o_id, no_d_id, no_w_id) VALUES (%d, %d, %d)", d.Next, c.district, t.home), nil); err != nil {
		return err
	}
	return s.Exec(ctx, "COMMIT", nil)
}

func mysqlPayment(ctx context.Context, s *bench.Tx, t *terminal, c mysqlCall) error {
	if err := s.Exec(ctx, "start transaction", nil); err != nil {
		return err
	}
	if err := s.Exec(ctx, fmt.Sprintf("UPDATE warehouse SET w_ytd = w_ytd + %d WHERE w_id = %d", c.amount, t.home), nil); err != nil {
		return err
	}
	ware, err := s.RawRow(ctx, fmt.Sprintf("SELECT w_street_1, w_street_2, w_city, w_state, w_zip, w_name FROM warehouse WHERE w_id = %d", t.home), nil)
	if err != nil {
		return err
	}
	if err := s.Exec(ctx, fmt.Sprintf("UPDATE district SET d_ytd = d_ytd + %d WHERE d_w_id = %d AND d_id = %d", c.amount, t.home, c.district), nil); err != nil {
		return err
	}
	district, err := s.RawRow(ctx, fmt.Sprintf("SELECT d_street_1, d_street_2, d_city, d_state, d_zip, d_name FROM district WHERE d_w_id = %d AND d_id = %d", t.home, c.district), nil)
	if err != nil {
		return err
	}
	if len(ware) != 6 || len(district) != 6 {
		return fmt.Errorf("payment warehouse/district missing")
	}
	where := fmt.Sprintf("c_w_id = %d AND c_d_id = %d AND c_id = %d", c.customerWarehouse, c.customerDistrict, c.customer)
	var person []any
	if c.byname == 1 {
		count, err := s.QueryValue[int64](ctx, fmt.Sprintf("SELECT count(c_id) FROM customer WHERE c_last = %s AND c_d_id = %d AND c_w_id = %d", mysqlQuote(c.name), c.customerDistrict, c.customerWarehouse), nil)
		if err != nil {
			return err
		}
		rows, err := s.RawRows(ctx, fmt.Sprintf("SELECT c_first, c_middle, c_id, c_street_1, c_street_2, c_city, c_state, c_zip, c_phone, c_credit, c_credit_lim, c_discount, c_balance, c_since FROM customer WHERE c_w_id = %d AND c_d_id = %d AND c_last = %s ORDER BY c_first", c.customerWarehouse, c.customerDistrict, mysqlQuote(c.name)), nil)
		if err != nil {
			return err
		}
		if count > 0 && int(count) <= len(rows) {
			person = rows[(count-1)/2]
			c.customer, err = strconv.Atoi(fmt.Sprint(person[2]))
			if err != nil {
				return err
			}
		}
		where = fmt.Sprintf("c_w_id = %d AND c_d_id = %d AND c_id = %d", c.customerWarehouse, c.customerDistrict, c.customer)
	} else {
		person, err = s.RawRow(ctx, "SELECT c_first, c_middle, c_last, c_street_1, c_street_2, c_city, c_state, c_zip, c_phone, c_credit, c_credit_lim, c_discount, c_balance, c_since FROM customer WHERE "+where, nil)
		if err != nil {
			return err
		}
	}
	balance := float64(c.amount)
	if len(person) > 12 {
		value, err := strconv.ParseFloat(fmt.Sprint(person[12]), 64)
		if err != nil {
			return err
		}
		balance += value
	}
	history := fmt.Sprint(ware[5]) + " " + fmt.Sprint(district[5])
	update := fmt.Sprintf("UPDATE customer SET c_balance = %.17g", balance)
	if len(person) > 9 && fmt.Sprint(person[9]) == "BC" {
		data, err := s.QueryValue[string](ctx, "SELECT c_data FROM customer WHERE "+where, nil)
		if err != nil {
			return err
		}
		prefix := fmt.Sprintf("p_c_id %d p_c_d_id %d p_c_w_id %d p_d_id %d p_w_id %d p_h_amount %.2f h_date %s h_data %s", c.customer, c.customerDistrict, c.customerWarehouse, c.district, t.home, float64(c.amount), c.date, history)
		combined := prefix + " " + data
		last := min(500-len(prefix)+1, len(combined))
		if last < 1 {
			last = 1
		}
		update += ", c_data = " + mysqlQuote(combined[1:last])
	}
	if err := s.Exec(ctx, update+" WHERE "+where, nil); err != nil {
		return err
	}
	if err := s.Exec(ctx, fmt.Sprintf("INSERT INTO history (h_c_d_id, h_c_w_id, h_c_id, h_d_id, h_w_id, h_date, h_amount, h_data) VALUES (%d, %d, %d, %d, %d, %s, %d, %s)", c.customerDistrict, c.customerWarehouse, c.customer, c.district, t.home, c.date, c.amount, mysqlQuote(history)), nil); err != nil {
		return err
	}
	return s.Exec(ctx, "COMMIT", nil)
}

func mysqlOrderStatus(ctx context.Context, s *bench.Tx, t *terminal, c mysqlCall) error {
	if err := s.Exec(ctx, "start transaction", nil); err != nil {
		return err
	}
	if c.byname == 1 {
		count, err := s.QueryValue[int64](ctx, fmt.Sprintf("SELECT count(c_id) FROM customer WHERE c_last = %s AND c_d_id = %d AND c_w_id = %d", mysqlQuote(c.name), c.district, t.home), nil)
		if err != nil {
			return err
		}
		if count%2 == 1 {
			count++
		}
		rows, err := s.RawRows(ctx, fmt.Sprintf("SELECT c_balance, c_first, c_middle, c_id FROM customer WHERE c_last = %s AND c_d_id = %d AND c_w_id = %d ORDER BY c_first", mysqlQuote(c.name), c.district, t.home), nil)
		if err != nil {
			return err
		}
		if count > 0 && int(count/2) <= len(rows) {
			c.customer, err = strconv.Atoi(fmt.Sprint(rows[count/2-1][3]))
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("order-status customer missing")
		}
	} else {
		if err := s.Exec(ctx, fmt.Sprintf("SELECT c_balance, c_first, c_middle, c_last FROM customer WHERE c_id = %d AND c_d_id = %d AND c_w_id = %d", c.customer, c.district, t.home), nil); err != nil {
			return err
		}
	}
	orders, err := s.RawRow(ctx, fmt.Sprintf("SELECT o_id, o_carrier_id, o_entry_d FROM (SELECT o_id, o_carrier_id, o_entry_d FROM orders where o_d_id = %d AND o_w_id = %d and o_c_id = %d ORDER BY o_id DESC) AS sb LIMIT 1", c.district, t.home, c.customer), nil)
	if err != nil {
		return err
	}
	order := "0"
	if len(orders) > 0 {
		order = fmt.Sprint(orders[0])
	}
	if err := s.Exec(ctx, fmt.Sprintf("SELECT ol_i_id, ol_supply_w_id, ol_quantity, ol_amount, ol_delivery_d FROM order_line WHERE ol_o_id = %s AND ol_d_id = %d AND ol_w_id = %d", order, c.district, t.home), nil); err != nil {
		return err
	}
	return s.Exec(ctx, "COMMIT", nil)
}

func mysqlDelivery(ctx context.Context, s *bench.Tx, t *terminal, c mysqlCall) error {
	if err := s.Exec(ctx, "start transaction", nil); err != nil {
		return err
	}
	for district := 1; district <= 10; district++ {
		rows, err := s.QueryValues[int64](ctx, fmt.Sprintf("SELECT no_o_id FROM new_order WHERE no_w_id = %d AND no_d_id = %d LIMIT 1", t.home, district), nil)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		order := rows[0]
		if err := s.Exec(ctx, fmt.Sprintf("DELETE FROM new_order WHERE no_w_id = %d AND no_d_id = %d AND no_o_id = %d", t.home, district, order), nil); err != nil {
			return err
		}
		customer, err := s.QueryValue[int64](ctx, fmt.Sprintf("SELECT o_c_id FROM orders WHERE o_id = %d AND o_d_id = %d AND o_w_id = %d", order, district, t.home), nil)
		if err != nil {
			return err
		}
		if err := s.Exec(ctx, fmt.Sprintf("UPDATE orders SET o_carrier_id = %d WHERE o_id = %d AND o_d_id = %d AND o_w_id = %d", c.carrier, order, district, t.home), nil); err != nil {
			return err
		}
		if err := s.Exec(ctx, fmt.Sprintf("UPDATE order_line SET ol_delivery_d = str_to_date(%s,'%%Y%%m%%d%%H%%i%%s') WHERE ol_o_id = %d AND ol_d_id = %d AND ol_w_id = %d", c.date, order, district, t.home), nil); err != nil {
			return err
		}
		total, err := s.QueryValue[float64](ctx, fmt.Sprintf("SELECT SUM(ol_amount) FROM order_line WHERE ol_o_id = %d AND ol_d_id = %d AND ol_w_id = %d", order, district, t.home), nil)
		if err != nil {
			return err
		}
		if err := s.Exec(ctx, fmt.Sprintf("UPDATE customer SET c_balance = c_balance + %.17g WHERE c_id = %d AND c_d_id = %d AND c_w_id = %d", total, customer, district, t.home), nil); err != nil {
			return err
		}
	}
	return s.Exec(ctx, "COMMIT", nil)
}
