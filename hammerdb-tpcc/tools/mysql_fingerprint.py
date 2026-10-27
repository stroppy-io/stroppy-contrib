#!/usr/bin/env python3
"""Print stable non-timestamp table fingerprints via the mysql CLI."""
import argparse
import hashlib
import subprocess

TABLES = {
    "warehouse": ("w_id", ""),
    "district": ("d_w_id,d_id", ""),
    "item": ("i_id", ""),
    "stock": ("s_w_id,s_i_id", ""),
    "customer": ("c_w_id,c_d_id,c_id", "c_since"),
    "history": ("h_c_w_id,h_c_d_id,h_c_id,h_amount,h_data", "h_date"),
    "orders": ("o_w_id,o_d_id,o_id", "o_entry_d"),
    "new_order": ("no_w_id,no_d_id,no_o_id", ""),
    "order_line": ("ol_w_id,ol_d_id,ol_o_id,ol_number", "ol_delivery_d"),
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("database")
    parser.add_argument("mysql", nargs=argparse.REMAINDER, help="mysql command and connection flags")
    args = parser.parse_args()
    command = args.mysql or ["mysql"]
    for table, (order, exclude) in sorted(TABLES.items()):
        sql = f"SELECT COLUMN_NAME FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='{table}' ORDER BY ORDINAL_POSITION"
        columns = subprocess.check_output(command + ["-N", "-B", args.database, "-e", sql], text=True).splitlines()
        columns = [column for column in columns if column != exclude and column != "id"]
        if not columns:
            raise ValueError(f"no columns in {table}")
        expressions = [f"IFNULL(HEX(`{column}`),'NULL')" for column in columns]
        sql = f"SELECT CONCAT_WS('|',{','.join(expressions)}) FROM `{table}` ORDER BY {order}"
        data = subprocess.check_output(command + ["-N", "-B", args.database, "-e", sql])
        print(f"{table}|{len(data.splitlines())}|{hashlib.sha256(data).hexdigest()}")


if __name__ == "__main__":
    main()
