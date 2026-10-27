package workload

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

var mysqlOutput = [...]string{
	"select @disc,@last,@credit,@dtax,@wtax,@next_o_id",
	"select @p_c_id,@p_c_last,@p_w_street_1,@p_w_street_2,@p_w_city,@p_w_state,@p_w_zip,@p_d_street_1,@p_d_street_2,@p_d_city,@p_d_state,@p_d_zip,@p_c_first,@p_c_middle,@p_c_street_1,@p_c_street_2,@p_c_city,@p_c_state,@p_c_zip,@p_c_phone,@p_c_since,@p_c_credit,@p_c_credit_lim,@p_c_discount,@p_c_balance,@p_c_data",
	"",
	"select @stock_count",
	"select @os_c_id,@os_c_last,@os_c_first,@os_c_middle,@os_c_balance,@os_o_id,@os_entdate,@os_o_carrier_id",
}

type mysqlCall struct {
	setup, text, output                                                                                string
	district, customer, customerWarehouse, customerDistrict, byname, amount, lines, carrier, threshold int
	name, date                                                                                         string
}

func (t *terminal) mysqlStatement(kind int, now time.Time) mysqlCall {
	r := t.random
	c := mysqlCall{date: now.Format("20060102150405"), output: mysqlOutput[kind]}
	stamp := "str_to_date(" + c.date + ",'%Y%m%d%H%i%s')"
	switch kind {
	case 0:
		c.district, c.customer, c.lines = r.number(1, 10), r.number(1, 3000), r.number(5, 15)
		c.setup = "set @next_o_id = 0"
		c.text = fmt.Sprintf("CALL NEWORD(%d,%d,%d,%d,%d,@disc,@last,@credit,@dtax,@wtax,@next_o_id,%s)", t.home, t.warehouses, c.district, c.customer, c.lines, stamp)
		if t.prepared {
			c.setup = fmt.Sprintf("set @no_w_id=%d,@w_id_input=%d,@no_d_id=%d,@no_c_id=%d,@ol_cnt=%d,@next_o_id=0,@date=%s", t.home, t.warehouses, c.district, c.customer, c.lines, stamp)
			c.text = "execute neword_st using @no_w_id,@w_id_input,@no_d_id,@no_c_id,@ol_cnt,@next_o_id,@date"
		}
	case 1:
		c.district = r.number(1, 10)
		x, y := r.number(1, 100), r.number(1, 100)
		c.customerDistrict, c.customerWarehouse = c.district, t.home
		if x > 85 {
			c.customerDistrict, c.customerWarehouse = r.number(1, 10), r.number(1, t.warehouses)
			for c.customerWarehouse == t.home && t.warehouses != 1 {
				c.customerWarehouse = r.number(1, t.warehouses)
			}
		}
		c.name, c.customer = lastname(r.nurand()), r.number(1, 3000)
		c.byname = 1
		if y > 60 {
			c.byname, c.name = 0, ""
		}
		c.amount = r.number(1, 5000)
		c.setup = fmt.Sprintf("set @p_c_id = %d, @p_c_last = '%s', @p_c_credit = 0, @p_c_balance = 0", c.customer, c.name)
		c.text = fmt.Sprintf("CALL PAYMENT(%d,%d,%d,%d,@p_c_id,%d,%d,@p_c_last,@p_w_street_1,@p_w_street_2,@p_w_city,@p_w_state,@p_w_zip,@p_d_street_1,@p_d_street_2,@p_d_city,@p_d_state,@p_d_zip,@p_c_first,@p_c_middle,@p_c_street_1,@p_c_street_2,@p_c_city,@p_c_state,@p_c_zip,@p_c_phone,@p_c_since,@p_c_credit,@p_c_credit_lim,@p_c_discount,@p_c_balance,@p_c_data,%s)", t.home, c.district, c.customerWarehouse, c.customerDistrict, c.byname, c.amount, stamp)
		if t.prepared {
			c.setup = fmt.Sprintf("set @p_w_id=%d,@p_d_id=%d,@p_c_w_id=%d,@p_c_d_id=%d,@p_c_id=%d,@byname=%d,@p_h_amount=%d,@p_c_last='%s',@p_c_credit=0,@p_c_balance=0,@h_date=%s", t.home, c.district, c.customerWarehouse, c.customerDistrict, c.customer, c.byname, c.amount, c.name, stamp)
			c.text = "execute payment_st using @p_w_id,@p_d_id,@p_c_w_id,@p_c_d_id,@p_c_id,@byname,@p_h_amount,@p_c_last,@p_c_credit,@p_c_balance,@h_date"
		}
	case 2:
		c.carrier = r.number(1, 10)
		c.text = fmt.Sprintf("CALL DELIVERY(%d,%d,%s)", t.home, c.carrier, stamp)
		if t.prepared {
			c.setup = fmt.Sprintf("set @d_w_id=%d,@d_o_carrier_id=%d,@timestamp=%s", t.home, c.carrier, stamp)
			c.text = "execute delivery_st using @d_w_id,@d_o_carrier_id,@timestamp"
		}
	case 3:
		c.threshold = r.number(10, 20)
		c.text = fmt.Sprintf("CALL SLEV(%d,%d,%d,@stock_count)", t.home, t.district, c.threshold)
		if t.prepared {
			c.setup = fmt.Sprintf("set @st_w_id=%d,@st_d_id=%d,@threshold=%d", t.home, t.district, c.threshold)
			c.text = "execute slev_st using @st_w_id,@st_d_id,@threshold"
		}
	default:
		c.district = r.number(1, 10)
		c.name, c.customer = lastname(r.nurand()), r.number(1, 3000)
		c.byname = 1
		if r.number(1, 100) > 60 {
			c.byname, c.name = 0, ""
		}
		c.setup = fmt.Sprintf("set @os_c_id = %d, @os_c_last = '%s'", c.customer, c.name)
		c.text = fmt.Sprintf("CALL OSTAT(%d,%d,@os_c_id,%d,@os_c_last,@os_c_first,@os_c_middle,@os_c_balance,@os_o_id,@os_entdate,@os_o_carrier_id)", t.home, c.district, c.byname)
		if t.prepared {
			c.setup = fmt.Sprintf("set @os_w_id=%d,@dos_d_id=%d,@os_c_id=%d,@byname=%d,@os_c_last='%s'", t.home, c.district, c.customer, c.byname, c.name)
			c.text = "execute ostat_st using @os_w_id,@dos_d_id,@os_c_id,@byname,@os_c_last"
		}
	}
	return c
}

func (c mysqlCall) run(ctx context.Context, session *bench.Tx) error {
	if c.setup != "" {
		if err := session.Exec(ctx, c.setup, nil); err != nil {
			return err
		}
	}
	if err := session.Exec(ctx, c.text, nil); err != nil {
		return err
	}
	if c.output != "" {
		return session.Exec(ctx, c.output, nil)
	}
	return nil
}

func (w *workload) createMySQLSchema(ctx context.Context, b *bench.Bench) error {
	invisiblePK := w.historyPK && strings.EqualFold(w.storageEngine, "innodb")
	if invisiblePK {
		version, err := b.QueryValue[string](ctx, "select version()", nil)
		if err != nil {
			return err
		}
		var major, minor, patch int
		if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
			return err
		}
		invisiblePK = major > 8 || major == 8 && (minor > 0 || patch >= 30)
	}
	schema := assetSQL("mysql_schema.sql")
	for _, statement := range schema.Section("create_schema") {
		if invisiblePK && strings.HasPrefix(statement, "CREATE TABLE `history`") {
			statement = schema.Require("history_pk", "history_pk").Text
		}
		statement = strings.ReplaceAll(statement, "$mysql_storage_engine", w.storageEngine)
		if w.partition && w.warehouses >= 200 && strings.HasPrefix(statement, "CREATE TABLE `order_line`") {
			statement += fmt.Sprintf("\nPARTITION BY HASH (`ol_w_id`) PARTITIONS %d", w.warehouses/100)
		}
		if err := b.Exec(ctx, statement, nil); err != nil {
			return err
		}
	}
	return nil
}
