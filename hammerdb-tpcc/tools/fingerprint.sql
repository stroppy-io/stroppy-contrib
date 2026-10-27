SELECT 'warehouse', count(*), md5(string_agg(md5(to_jsonb(t)::text), '' ORDER BY w_id)) FROM warehouse t
UNION ALL SELECT 'district', count(*), md5(string_agg(md5(to_jsonb(t)::text), '' ORDER BY d_w_id,d_id)) FROM district t
UNION ALL SELECT 'item', count(*), md5(string_agg(md5(to_jsonb(t)::text), '' ORDER BY i_id)) FROM item t
UNION ALL SELECT 'stock', count(*), md5(string_agg(md5(to_jsonb(t)::text), '' ORDER BY s_w_id,s_i_id)) FROM stock t
UNION ALL SELECT 'customer', count(*), md5(string_agg(md5((to_jsonb(t)-'c_since')::text), '' ORDER BY c_w_id,c_d_id,c_id)) FROM customer t
UNION ALL SELECT 'history', count(*), md5(string_agg(md5((to_jsonb(t)-'h_date')::text), '' ORDER BY h_c_w_id,h_c_d_id,h_c_id)) FROM history t
UNION ALL SELECT 'orders', count(*), md5(string_agg(md5((to_jsonb(t)-'o_entry_d')::text), '' ORDER BY o_w_id,o_d_id,o_id)) FROM orders t
UNION ALL SELECT 'new_order', count(*), md5(string_agg(md5(to_jsonb(t)::text), '' ORDER BY no_w_id,no_d_id,no_o_id)) FROM new_order t
UNION ALL SELECT 'order_line', count(*), md5(string_agg(md5((to_jsonb(t)-'ol_delivery_d')::text), '' ORDER BY ol_w_id,ol_d_id,ol_o_id,ol_number)) FROM order_line t
ORDER BY 1;
